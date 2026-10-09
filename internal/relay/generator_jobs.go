package relay

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"github.com/kreuzhofer/restreamer/internal/config"
	"github.com/kreuzhofer/restreamer/internal/mediaauthor"
)

const generatorRenderer = "go-png-ffmpeg-v3"
const maxGeneratorJobs = 200
const maxGeneratorOutstanding = 8
const maxGeneratorSeconds = 600
const maxGeneratorOutputBytes = 512 << 20
const generatorTimeout = 15 * time.Minute

// GenerationJob captures immutable inputs. Only state, progress, error and the
// resulting media identity change after acceptance; drafts are never reread.
type GenerationJob struct {
	MixGain        *float64           `json:"mix_gain,omitempty"`
	ID             string             `json:"id"`
	Sequence       uint64             `json:"sequence"`
	QueuePosition  int                `json:"queue_position,omitempty"`
	Message        string             `json:"message,omitempty"`
	RetryOf        string             `json:"retry_of,omitempty"`
	State          string             `json:"state"`
	Progress       int                `json:"progress"`
	Error          string             `json:"error,omitempty"`
	DesignRevision string             `json:"design_revision"`
	Design         mediaauthor.Design `json:"design_snapshot"`
	ThemeSnapshot  *mediaauthor.Theme `json:"theme_snapshot,omitempty"`
	Profile        config.BRBProfile  `json:"profile"`
	Renderer       string             `json:"renderer"`
	Duration       float64            `json:"duration"`
	MediaRevision  string             `json:"media_revision,omitempty"`
	CreatedAt      string             `json:"created_at"`
}

func (g *generatorStore) jobPath(id string) string       { return filepath.Join(g.jobsRoot, id+".json") }
func (g *generatorStore) saveJob(j *GenerationJob) error { return writeState(g.jobPath(j.ID), j) }
func (g *generatorStore) loadJobs() error {
	files, err := os.ReadDir(g.jobsRoot)
	if err != nil {
		return err
	}
	for _, f := range files {
		if filepath.Ext(f.Name()) != ".json" {
			continue
		}
		if len(g.jobs) >= maxGeneratorJobs {
			return errors.New("generator job history limit exceeded")
		}
		var j GenerationJob
		if readGeneratorJob(filepath.Join(g.jobsRoot, f.Name()), &j) != nil || !validDesignID(j.ID) || f.Name() != j.ID+".json" || !designBounds(j.Design) || j.Profile.Validate() != nil {
			return errors.New("invalid saved generator job")
		}
		switch j.State {
		case "queued", "running", "cancelling":
			j.State = "interrupted"
			j.Message = ""
			j.Error = "Server stopped during generation. Retry this captured revision explicitly when ready."
			if g.saveJob(&j) != nil {
				return errors.New("cannot record interrupted generation")
			}
		case "ready", "failed", "cancelled", "interrupted":
		default:
			return errors.New("invalid saved generator state")
		}
		g.jobs[j.ID] = &j
		g.sequence = max(g.sequence, j.Sequence)
	}
	// Assign admission numbers to records created before queue support. Sorting
	// by their original timestamps gives legacy records a stable order.
	legacy := make([]*GenerationJob, 0)
	for _, j := range g.jobs {
		if j.Sequence == 0 {
			legacy = append(legacy, j)
		}
	}
	sort.Slice(legacy, func(i, j int) bool {
		if legacy[i].CreatedAt == legacy[j].CreatedAt {
			return legacy[i].ID < legacy[j].ID
		}
		return legacy[i].CreatedAt < legacy[j].CreatedAt
	})
	for _, j := range legacy {
		g.sequence++
		j.Sequence = g.sequence
		if g.saveJob(j) != nil {
			return errors.New("cannot save generation queue order")
		}
	}
	// Only scratch directories are disposable. Captured metadata and retained
	// revisions survive restart; interrupted output can never become ready.
	entries, err := os.ReadDir(g.workRoot)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if validDesignID(entry.Name()) {
			if os.RemoveAll(filepath.Join(g.workRoot, entry.Name())) != nil {
				return errors.New("cannot clear interrupted generator output")
			}
		}
	}
	return nil
}
func (g *generatorStore) jobList() []GenerationJob {
	g.jobsMu.Lock()
	defer g.jobsMu.Unlock()
	out := make([]GenerationJob, 0, len(g.jobs))
	for _, j := range g.jobs {
		out = append(out, *j)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Sequence < out[j].Sequence })
	position := 0
	for i := range out {
		if out[i].State == "queued" {
			position++
			out[i].QueuePosition = position
		}
	}
	return out
}
func generationIssues(d mediaauthor.Design, p config.BRBProfile, themes ...mediaauthor.Theme) []mediaauthor.Issue {
	theme := mediaauthor.RetroTheme(1)
	if len(themes) > 0 {
		theme = themes[0]
	}
	issues := mediaauthor.ValidateWithTheme(d, theme, p.Width, p.Height)
	if p.Validate() != nil || p.Width > 1920 || p.Height > 1080 || p.FPS > 30 {
		issues = append(issues, mediaauthor.Issue{Field: "profile", Message: "Generation supports active profiles up to 1920 × 1080 at 24, 25 or 30 fps. Change the profile explicitly before generating."})
	}
	issues = append(issues, mediaauthor.ValidateTiming(d, p.FPS)...)
	return issues
}
func fmtSceneField(i int, field string) string { return "scenes." + strconv.Itoa(i) + "." + field }
func (s *Server) runGenerator(ctx context.Context) {
	g := s.generator
	for {
		if ctx.Err() != nil {
			return
		}
		g.jobsMu.Lock()
		var job *GenerationJob
		for _, j := range g.jobs {
			if j.State == "queued" && (job == nil || j.Sequence < job.Sequence) {
				job = j
			}
		}
		if job == nil {
			g.jobsMu.Unlock()
			select {
			case <-ctx.Done():
				return
			case <-g.wake:
			}
			continue
		}
		jobCtx, cancel := context.WithTimeout(ctx, generatorTimeout)
		g.cancel = cancel
		job.State = "running"
		job.Message = "Waiting for media preparation; live delivery continues."
		if g.saveJob(job) != nil {
			job.State = "failed"
			job.Message = ""
			job.Error = "Cannot save generation state; check storage."
			g.cancel = nil
			cancel()
			g.jobsMu.Unlock()
			continue
		}
		snapshot := *job
		g.jobsMu.Unlock()
		var revision string
		release, err := s.preparation.acquire(jobCtx)
		if err == nil {
			g.jobsMu.Lock()
			job.Message = "Rendering captured revision."
			g.jobsMu.Unlock()
			revision, err = s.renderGenerator(jobCtx, &snapshot, func(percent int) {
				g.jobsMu.Lock()
				defer g.jobsMu.Unlock()
				if job.State == "running" {
					job.Progress = percent
				}
			})
			release()
		}
		g.jobsMu.Lock()
		job.MixGain = snapshot.MixGain
		switch {
		case job.State == "cancelling":
			job.State = "cancelled"
			job.Error = "Generation cancelled; previous media is unchanged."
		case ctx.Err() != nil:
			job.State = "interrupted"
			job.Error = "Server stopped during generation. Retry this captured revision explicitly when ready."
		case errors.Is(jobCtx.Err(), context.DeadlineExceeded):
			job.State = "failed"
			job.Error = "Generation exceeded its 15 minute time limit. Shorten the design or reduce the active profile."
		case err != nil:
			job.State = "failed"
			job.Error = err.Error()
			job.MediaRevision = revision
		default:
			job.State = "ready"
			job.Progress = 100
			job.MediaRevision = revision
		}
		job.Message = ""
		cancel()
		g.cancel = nil
		if g.saveJob(job) != nil {
			job.State = "failed"
			job.Error = "Cannot save completed generation state; check storage. Prepared revisions remain in the media library."
		}
		g.jobsMu.Unlock()
	}
}
func newGenerationJob(d mediaauthor.Design, p config.BRBProfile, themes ...mediaauthor.Theme) (GenerationJob, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return GenerationJob{}, err
	}
	j := GenerationJob{ID: hex.EncodeToString(id[:]), State: "queued", Design: d, Profile: p, Renderer: generatorRenderer, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	theme := mediaauthor.RetroTheme(1)
	if len(themes) > 0 {
		theme = themes[0]
	}
	j.ThemeSnapshot = &theme
	j.Duration = mediaauthor.SequenceDuration(d, p.FPS)
	encoded, _ := json.Marshal(struct {
		Design   mediaauthor.Design
		Profile  config.BRBProfile
		Renderer string
		Theme    mediaauthor.Theme
	}{d, p, j.Renderer, theme})
	hash := sha256.Sum256(encoded)
	j.DesignRevision = hex.EncodeToString(hash[:])
	return j, nil
}

// Jobs wrap a draft with captured profile/revision metadata. Keep a separate
// bound so every admitted 64 KiB draft can be recovered without truncation.
func readGeneratorJob(path string, dst *GenerationJob) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	dec := json.NewDecoder(io.LimitReader(f, maxDesignBytes+(16<<10)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	if dec.Decode(new(any)) != io.EOF {
		return errors.New("invalid generation metadata")
	}
	return nil
}
