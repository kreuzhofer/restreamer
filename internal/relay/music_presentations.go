package relay

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/kreuzhofer/restreamer/internal/config"
	"github.com/kreuzhofer/restreamer/internal/mediaauthor"
)

const maxPresentationBytes int64 = 2 << 30

// MusicPresentation pins the visual and audio inputs independently of the
// downloadable one-pass mux. PCM files are disk-backed, never loaded wholesale.
type MusicPresentation struct {
	VisualRevision string                 `json:"visual_revision"`
	Tracks         []mediaauthor.AssetRef `json:"tracks"`
	Playlist       *mediaauthor.AssetRef  `json:"playlist,omitempty"`
	Mode           string                 `json:"mode"`
	MusicSamples   int64                  `json:"music_samples"`
	SceneSamples   int64                  `json:"scene_samples"`
	MusicDigest    string                 `json:"music_digest"`
	SceneDigest    string                 `json:"scene_digest"`
	Gain           float64                `json:"gain"`
}

func (g *generatorStore) captureMusicPresentation(j *GenerationJob) error {
	if j.Kind == "" && j.Design.DurationMode == "" && (j.Design.Soundtrack == nil || j.Design.Soundtrack.Playlist == nil) {
		return nil
	}
	refs, err := g.musicRefs(j.Design.Soundtrack)
	if err != nil {
		return err
	}
	j.MusicTracks = refs
	if j.Design.DurationMode == "music" {
		samples, err := g.playlistTrackSamples(refs)
		if err != nil {
			return err
		}
		frames := (samples*int64(j.Profile.FPS) + musicSampleRate - 1) / musicSampleRate
		j.Duration = float64(frames) / float64(j.Profile.FPS)
	}
	return nil
}
func visualFingerprint(j GenerationJob) string {
	d := j.Design
	d.Soundtrack = nil
	d.DurationMode = ""
	d.ID = ""
	d.Name = ""
	d.Version = 0
	d.UpdatedAt = ""
	data, _ := json.Marshal(struct {
		Design   mediaauthor.Design
		Profile  config.BRBProfile
		Theme    *mediaauthor.Theme
		Renderer string
	}{d, j.Profile, j.ThemeSnapshot, j.Renderer})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
func (s *Server) renderPresentationJob(ctx context.Context, j *GenerationJob, progress func(int)) (string, error) {
	if j.Kind == "" && j.Design.DurationMode == "" && (j.Design.Soundtrack == nil || j.Design.Soundtrack.Playlist == nil) {
		return s.renderGenerator(ctx, j, progress)
	}
	j.VisualFingerprint = visualFingerprint(*j)
	if j.Kind == "brb" {
		raw, _ := json.Marshal(struct {
			Digest  string
			Profile config.BRBProfile
		}{j.BRBDigest, j.Profile})
		sum := sha256.Sum256(raw)
		j.VisualFingerprint = hex.EncodeToString(sum[:])
	}
	g := s.generator
	g.jobsMu.Lock()
	for _, old := range g.jobs {
		if old.ID != j.ID && old.VisualFingerprint == j.VisualFingerprint && old.VisualRevision != "" {
			j.VisualRevision = old.VisualRevision
			break
		}
	}
	g.jobsMu.Unlock()
	if j.VisualRevision != "" {
		clip, _, err := s.library.openRevision(j.VisualRevision)
		if err != nil {
			return "", errors.New("Reusable visual media is unavailable; restore it or change the design before preparing again")
		}
		clip.reader.close()
		j.ReusedVisual = true
	} else {
		base := *j
		base.Design.Soundtrack = nil
		base.Design.DurationMode = ""
		base.Duration = mediaauthor.SequenceDuration(base.Design, base.Profile.FPS)
		var revision string
		var err error
		if j.Kind == "brb" {
			revision, err = s.retainBRBVisual(ctx, j)
		} else {
			revision, err = s.renderGenerator(ctx, &base, func(n int) { progress(n * 60 / 100) })
		}
		if err != nil {
			return "", err
		}
		j.VisualRevision = revision
	}
	if j.Design.Soundtrack == nil {
		progress(100)
		return j.VisualRevision, nil
	}
	dir := filepath.Join(g.workRoot, j.ID)
	if err := os.Mkdir(dir, 0700); err != nil {
		return "", errors.New("Cannot create audio preparation workspace")
	}
	defer os.RemoveAll(dir)
	music := filepath.Join(dir, "music.pcm")
	samples, musicPeak, err := s.preparePresentationMusic(ctx, *j, music)
	if err != nil {
		return "", err
	}
	visual, _, err := s.library.openRevision(j.VisualRevision)
	if err != nil {
		return "", err
	}
	defer visual.reader.close()
	sceneFrames := visual.reader.index.Frames
	sceneSamples := sampleBoundary(sceneFrames, musicSampleRate, j.Profile.FPS)
	scene := filepath.Join(dir, "scene.pcm")
	args := append(generatorBaseArgs(), "-threads", "2", "-protocol_whitelist", "file,pipe", "-i", s.library.revisionPath(j.VisualRevision), "-map", "0:a:0", "-af", fmt.Sprintf("aresample=48000,apad,atrim=end_sample=%d", sceneSamples), "-ac", "2", "-ar", "48000", "-c:a", "pcm_s16le", "-threads", "2", "-f", "s16le", scene)
	if exec.CommandContext(ctx, "ffmpeg", args...).Run() != nil {
		return "", errors.New("Cannot prepare retained scene audio")
	}
	scenePeak, err := pcmPeak(ctx, scene, sceneSamples)
	if err != nil {
		return "", err
	}
	gain := 1 / math.Max(1, scenePeak+musicPeak)
	j.MixGain = &gain
	mode := j.Design.DurationMode
	if mode == "" {
		mode = "fixed"
	}
	presentation := MusicPresentation{VisualRevision: j.VisualRevision, Tracks: j.MusicTracks, Playlist: j.Design.Soundtrack.Playlist, Mode: mode, MusicSamples: samples, SceneSamples: sceneSamples, Gain: gain}
	presentation.MusicDigest, err = mediaDigest(music)
	if err != nil {
		return "", err
	}
	presentation.SceneDigest, err = mediaDigest(scene)
	if err != nil {
		return "", err
	}
	progress(70)
	output := filepath.Join(dir, "presentation.flv")
	frames := int(math.Round(j.Duration * float64(j.Profile.FPS)))
	if j.Design.DurationMode != "music" {
		if err := s.exportFixedPresentation(ctx, *j, scene, music, output, gain); err != nil {
			return "", err
		}
	} else {
		// Re-time copied encoded packets by frame count; no video decoder or encoder.
		args = generatorBaseArgs()
		args = append(args, "-stream_loop", "-1", "-protocol_whitelist", "file,pipe", "-i", s.library.revisionPath(j.VisualRevision))
		args = append(args, presentationAudioInputs(scene, music, j.Design.DurationMode == "music" || j.Design.Soundtrack.Mode == "repeat")...)
		filter := fmt.Sprintf("[1:a][2:a]amix=inputs=2:duration=longest:normalize=0,volume=%.12f,apad,atrim=end_sample=%d[a]", gain, sampleBoundary(frames, musicSampleRate, j.Profile.FPS))
		args = append(args, "-filter_complex", filter, "-map", "0:v:0", "-map", "[a]", "-c:v", "copy", "-bsf:v", fmt.Sprintf("setts=ts=N/(%d*TB)", j.Profile.FPS), "-frames:v", strconv.Itoa(frames), "-t", decimal(j.Duration), "-c:a", "aac", "-ar", strconv.Itoa(j.Profile.SampleRate), "-ac", "2", "-b:a", "128k", "-threads", "2", "-fs", strconv.FormatInt(maxPresentationBytes, 10), "-f", "flv", output)
		if exec.CommandContext(ctx, "ffmpeg", args...).Run() != nil {
			return "", errors.New("Cannot prepare presentation export; check audio and available disk space")
		}
	}
	idx, err := indexClipContext(ctx, output)
	if err != nil {
		return "", errors.New("Presentation export is incomplete")
	}
	if idx.Size >= maxPresentationBytes {
		return "", errors.New("Presentation exceeds the 2 GiB export limit; shorten the playlist")
	}
	if err := applyGeneratedTiming(idx, &GeneratedTiming{Version: 1, Frames: frames}, j.Profile); err != nil {
		return "", err
	}
	digest, err := mediaDigest(output)
	if err != nil {
		return "", err
	}
	revision := MediaRevision{LibraryID: "generator-" + j.ID, Name: j.Design.Name, State: "ready", Duration: j.Duration, Bytes: idx.Size, Profile: j.Profile, Timing: &GeneratedTiming{Version: 1, Frames: frames}, ContentDigest: digest, Presentation: &presentation, index: idx}
	revision.ID = presentationIdentity(revision)
	if err := s.retainPresentation(&revision, output, scene, music); err != nil {
		return "", err
	}
	progress(100)
	return revision.ID, nil
}
func presentationAudioInputs(scene, music string, repeat bool) []string {
	args := []string{"-stream_loop", "-1", "-f", "s16le", "-ar", "48000", "-ac", "2", "-protocol_whitelist", "file,pipe", "-i", scene}
	if repeat {
		args = append(args, "-stream_loop", "-1")
	}
	return append(args, "-f", "s16le", "-ar", "48000", "-ac", "2", "-protocol_whitelist", "file,pipe", "-i", music)
}
func presentationIdentity(r MediaRevision) string {
	data, _ := json.Marshal(struct {
		Digest       string
		Profile      config.BRBProfile
		Timing       *GeneratedTiming
		Presentation *MusicPresentation
	}{r.ContentDigest, r.Profile, r.Timing, r.Presentation})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
func (l *videoLibrary) presentationPCM(id, kind string) string {
	return filepath.Join(l.root, "presentations", id+"-"+kind+".pcm")
}
func (s *Server) retainPresentation(r *MediaRevision, export, scene, music string) error {
	l := s.library
	if err := os.MkdirAll(filepath.Join(l.root, "presentations"), 0700); err != nil {
		return errors.New("Cannot create presentation storage")
	}
	created := []string{}
	published := false
	defer func() {
		if !published {
			for _, path := range created {
				os.Remove(path)
			}
		}
	}()
	// PCM links are recovery markers: publish the export only after at least one exists.
	for _, pair := range [][2]string{{scene, l.presentationPCM(r.ID, "scene")}, {music, l.presentationPCM(r.ID, "music")}, {export, l.revisionPath(r.ID)}} {
		src, dst := pair[0], pair[1]
		if err := os.Link(src, dst); err == nil {
			created = append(created, dst)
		} else if !errors.Is(err, os.ErrExist) {
			return errors.New("Cannot retain presentation files")
		}
		want, err := mediaDigest(src)
		if err != nil {
			return err
		}
		got, err := mediaDigest(dst)
		if err != nil || got != want {
			return errors.New("Retained presentation files changed")
		}
	}
	if err := writeState(filepath.Join(l.root, "revisions", r.ID+".json"), r); err != nil {
		return errors.New("Cannot retain presentation metadata")
	}
	published = true
	l.mu.Lock()
	defer l.mu.Unlock()
	l.revisions[r.ID] = r
	if l.profile != r.Profile {
		return errors.New("Profile changed during preparation; prepare for the current profile")
	}
	return nil
}
func (s *Server) preparePresentationMusic(ctx context.Context, j GenerationJob, path string) (int64, float64, error) {
	g := s.generator
	refs, m := j.MusicTracks, j.Design.Soundtrack
	out, err := os.Create(path)
	if err != nil {
		return 0, 0, err
	}
	defer out.Close()
	var samples int64
	for _, ref := range refs {
		if err := ctx.Err(); err != nil {
			return 0, 0, err
		}
		g.mu.Lock()
		meta, ok := g.assetRevision(ref)
		source, openErr := os.Open(g.assetPath(ref))
		g.mu.Unlock()
		if !ok || openErr != nil {
			if source != nil {
				source.Close()
			}
			return 0, 0, errors.New("Captured music revision is unavailable")
		}
		err = verifyMusicRevision(source, meta)
		if err == nil {
			_, err = source.Seek(44, io.SeekStart)
		}
		if err == nil {
			_, err = io.CopyN(out, source, meta.Samples*4)
		}
		if err == nil {
			_, err = source.Seek(0, io.SeekStart)
		}
		if err == nil {
			err = verifyMusicRevision(source, meta)
		}
		source.Close()
		if err != nil {
			return 0, 0, errors.New("Cannot read captured music revision")
		}
		samples += meta.Samples
		if samples > maxPlaylistSamples {
			return 0, 0, errors.New("Music exceeds the one-hour presentation limit")
		}
	}
	if samples == 0 {
		return 0, 0, errors.New("Choose at least one music track")
	}
	if err := out.Close(); err != nil {
		return 0, 0, err
	}
	audible := samples
	if j.Design.DurationMode != "music" {
		count := sampleBoundary(int(math.Round(mediaauthor.CompositionDuration(j.Design, j.Profile.FPS)*float64(j.Profile.FPS))), musicSampleRate, j.Profile.FPS)
		if err := fitPresentationMusic(ctx, path, samples, count, m.Mode == "repeat"); err != nil {
			return 0, 0, err
		}
		samples = count
		if m.Mode == "repeat" {
			audible = count
		} else {
			audible = min(audible, count)
		}
	}
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()
	fadeIn := int64(math.Round(m.FadeInSeconds * musicSampleRate))
	fadeOut := int64(math.Round(m.FadeOutSeconds * musicSampleRate))
	buf := make([]byte, 32768)
	peak := 0.0
	for offset := int64(0); offset < samples*4; offset += int64(len(buf)) {
		if err := ctx.Err(); err != nil {
			return 0, 0, err
		}
		n := int(min(int64(len(buf)), samples*4-offset))
		if _, err := f.ReadAt(buf[:n], offset); err != nil {
			return 0, 0, err
		}
		for i := 0; i < n; i += 2 {
			frame := (offset + int64(i)) / 4
			gain := m.Volume() / 100
			if fadeIn > 1 && frame < fadeIn {
				gain *= float64(frame) / float64(fadeIn-1)
			}
			if fadeOut > 1 && frame >= audible-fadeOut {
				gain *= float64(max(int64(0), audible-1-frame)) / float64(fadeOut-1)
			}
			value := int16(math.Round(float64(int16(binary.LittleEndian.Uint16(buf[i:]))) * gain))
			binary.LittleEndian.PutUint16(buf[i:], uint16(value))
			peak = math.Max(peak, math.Abs(float64(value))/32767)
		}
		if _, err := f.WriteAt(buf[:n], offset); err != nil {
			return 0, 0, err
		}
	}
	if err := f.Sync(); err != nil {
		return 0, 0, err
	}
	if j.Design.DurationMode != "music" && j.Design.Stage == "ending" {
		audioJob := j
		audioJob.Profile.SampleRate = musicSampleRate
		if _, err := applyGeneratorEndingFade(ctx, audioJob, path, int(math.Round(j.Duration*float64(j.Profile.FPS)))); err != nil {
			return 0, 0, err
		}
	}
	if j.Design.DurationMode != "music" && j.Design.Stage == "prestream" {
		plan, _ := mediaauthor.PlanTiming(j.Design, j.Profile.FPS)
		if plan.LoopFrames > 0 {
			audioProfile := j.Profile
			audioProfile.SampleRate = musicSampleRate
			if err := rotateCircularPCM(ctx, plan, audioProfile, path, filepath.Dir(path), maxGeneratorOutputBytes); err != nil {
				return 0, 0, err
			}
			samples = sampleBoundary(plan.Frames, musicSampleRate, j.Profile.FPS)
		}
	}
	return samples, peak, nil
}
func pcmPeak(ctx context.Context, path string, samples int64) (float64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil || stat.Size() != samples*4 {
		return 0, errors.New("Prepared audio has an incomplete sample count")
	}
	buf := make([]byte, 32768)
	peak := 0.0
	for {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		n, err := f.Read(buf)
		for i := 0; i+1 < n; i += 2 {
			peak = math.Max(peak, math.Abs(float64(int16(binary.LittleEndian.Uint16(buf[i:]))))/32767)
		}
		if errors.Is(err, io.EOF) {
			return peak, nil
		}
		if err != nil {
			return 0, err
		}
	}
}

func validatePresentation(r MediaRevision) error {
	p := r.Presentation
	if p == nil {
		if r.ContentDigest != "" {
			return errors.New("Unexpected presentation digest")
		}
		return nil
	}
	if !validRevisionID(p.VisualRevision) || p.VisualRevision == r.ID || !validRevisionID(r.ContentDigest) || !validRevisionID(p.MusicDigest) || !validRevisionID(p.SceneDigest) || r.Timing == nil || r.Bytes <= 0 || r.Bytes > maxPresentationBytes || p.SceneSamples <= 0 || p.SceneSamples > 600*musicSampleRate || p.MusicSamples <= 0 || p.MusicSamples > maxPlaylistSamples || math.IsNaN(p.Gain) || math.IsInf(p.Gain, 0) || p.Gain <= 0 || p.Gain > 1 || (p.Mode != "music" && p.Mode != "fixed") || len(p.Tracks) == 0 || len(p.Tracks) > maxPlaylistTracks || presentationIdentity(r) != r.ID {
		return errors.New("Invalid saved presentation")
	}
	for _, ref := range p.Tracks {
		if !validDesignID(ref.ID) || ref.Revision < 1 || ref.Revision > maxAssetRevisions {
			return errors.New("Invalid presentation music reference")
		}
	}
	if p.Playlist != nil && (!validDesignID(p.Playlist.ID) || p.Playlist.Revision < 1 || p.Playlist.Revision > maxPlaylistRevisions) {
		return errors.New("Invalid presentation playlist")
	}
	return nil
}

// Fixed mode retains its audible-length and fade contract without expanding PCM
// in memory. A sparse tail is silence; repeats copy bounded chunks from disk.
func fitPresentationMusic(ctx context.Context, path string, original, count int64, repeat bool) error {
	source, err := os.Open(path)
	if err != nil {
		return err
	}
	defer source.Close()
	next := path + ".fit"
	out, err := os.Create(next)
	if err != nil {
		return err
	}
	defer os.Remove(next)
	defer out.Close()
	written := int64(0)
	for written < count {
		if err := ctx.Err(); err != nil {
			return err
		}
		n := min(original, count-written)
		if _, err := source.Seek(0, io.SeekStart); err != nil {
			return err
		}
		if _, err := io.CopyN(out, source, n*4); err != nil {
			return err
		}
		written += n
		if !repeat {
			if err := out.Truncate(count * 4); err != nil {
				return err
			}
			break
		}
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Rename(next, path)
}

func (s *Server) exportFixedPresentation(ctx context.Context, j GenerationJob, scene, music, output string, gain float64) error {
	frames := int(math.Round(j.Duration * float64(j.Profile.FPS)))
	count := sampleBoundary(frames, j.Profile.SampleRate, j.Profile.FPS)
	mixed := filepath.Join(filepath.Dir(output), "mixed.pcm")
	args := append(generatorBaseArgs(), presentationAudioInputs(scene, music, false)...)
	filter := fmt.Sprintf("[0:a][1:a]amix=inputs=2:duration=longest:normalize=0,volume=%.12f,aresample=%d,apad,atrim=end_sample=%d[a]", gain, j.Profile.SampleRate, count)
	args = append(args, "-filter_complex", filter, "-map", "[a]", "-ac", "2", "-ar", strconv.Itoa(j.Profile.SampleRate), "-c:a", "pcm_s16le", "-f", "s16le", mixed)
	if exec.CommandContext(ctx, "ffmpeg", args...).Run() != nil {
		return errors.New("Cannot prepare fixed presentation audio")
	}
	preroll := 0.0
	if j.Design.Stage == "prestream" || j.Kind == "brb" {
		if err := warmCircularPCM(ctx, mixed, count, maxGeneratorOutputBytes); err != nil {
			return err
		}
		preroll = float64(cyclicWarmSamples) / float64(j.Profile.SampleRate)
	}
	args = generatorBaseArgs()
	args = append(args, "-protocol_whitelist", "file,pipe", "-i", s.library.revisionPath(j.VisualRevision), "-f", "s16le", "-ar", strconv.Itoa(j.Profile.SampleRate), "-ac", "2", "-i", mixed, "-map", "0:v:0", "-map", "1:a:0", "-c:v", "copy", "-bsf:v", fmt.Sprintf("setts=ts=N/(%d*TB)+%s/TB", j.Profile.FPS, decimal(preroll)), "-t", decimal(j.Duration+2*preroll), "-c:a", "aac", "-ar", strconv.Itoa(j.Profile.SampleRate), "-ac", "2", "-b:a", "128k", "-threads", "2", "-fs", strconv.FormatInt(maxPresentationBytes, 10), "-f", "flv", output)
	if exec.CommandContext(ctx, "ffmpeg", args...).Run() != nil {
		return errors.New("Cannot export fixed presentation audio and video")
	}
	if preroll > 0 {
		return retainCircularFLV(ctx, output, time.Duration(frames)*time.Second/time.Duration(j.Profile.FPS))
	}
	return nil
}

// A process can stop between linking prepared files and publishing metadata.
// Only these server-owned presentation paths are disposable without a catalog
// entry; uploaded assets and valid retained revisions are never candidates.
func (l *videoLibrary) recoverPresentationFiles() error {
	root := filepath.Join(l.root, "presentations")
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return errors.New("Cannot inspect interrupted presentation storage")
	}
	ids := map[string]bool{}
	for _, entry := range entries {
		for _, suffix := range []string{"-scene.pcm", "-music.pcm"} {
			id := strings.TrimSuffix(entry.Name(), suffix)
			if strings.HasSuffix(entry.Name(), suffix) && validRevisionID(id) && l.revisions[id] == nil {
				ids[id] = true
			}
		}
	}
	for id := range ids {
		for _, path := range []string{l.presentationPCM(id, "scene"), l.presentationPCM(id, "music"), l.revisionPath(id)} {
			if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return errors.New("Cannot clean interrupted presentation files")
			}
		}
	}
	return nil
}
