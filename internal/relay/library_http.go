package relay

import (
	"encoding/json"
	"errors"
	"io"
	"math"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

func (s *Server) libraryStatus(w http.ResponseWriter, r *http.Request) {
	if s.library == nil {
		http.Error(w, "Enable BRB to use the video library and its output profile", 409)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.library.status())
}
func (s *Server) libraryUpload(w http.ResponseWriter, r *http.Request) {
	if !controlOriginAllowed(r) {
		http.Error(w, "Control requests require same origin", 403)
		return
	}
	l := s.library
	if l == nil {
		http.Error(w, "Enable BRB before uploading videos", 409)
		return
	}
	if !l.uploadMu.TryLock() {
		http.Error(w, "Another video upload is in progress", 409)
		return
	}
	defer l.uploadMu.Unlock()
	_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(2 * time.Hour))
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(2 * time.Hour))
	r.Body = http.MaxBytesReader(w, r.Body, l.limit+(1<<20))
	defer r.Body.Close()
	reader, err := r.MultipartReader()
	if err != nil {
		http.Error(w, "Expected one multipart MP4 file", 400)
		return
	}
	part, err := reader.NextPart()
	if err != nil || part.FormName() != "file" || !validClipName(part.FileName()) {
		http.Error(w, "Choose an MP4 with a filename up to 180 bytes, without path separators or control characters", 400)
		return
	}
	name := part.FileName()
	temp, err := os.CreateTemp(filepath.Join(l.root, "originals"), ".upload-*")
	if err != nil {
		http.Error(w, "Cannot create upload; check storage", 507)
		return
	}
	defer os.Remove(temp.Name())
	n, copyErr := io.Copy(temp, io.LimitReader(part, l.limit+1))
	closeErr := temp.Close()
	if n > l.limit {
		http.Error(w, "MP4 exceeds the configured upload limit", 413)
		return
	}
	if copyErr != nil || closeErr != nil {
		http.Error(w, "Upload interrupted or storage unavailable; original library is unchanged", 400)
		return
	}
	if n == 0 {
		http.Error(w, "MP4 is empty", 400)
		return
	}
	if _, err = reader.NextPart(); err != io.EOF {
		http.Error(w, "Upload exactly one MP4 file", 400)
		return
	}
	// Atomic publication without replacing existing files (including symlinks).
	if err = os.Link(temp.Name(), filepath.Join(l.root, "originals", name)); err != nil {
		if errors.Is(err, os.ErrExist) {
			http.Error(w, "A file with that name already exists; rename the upload", 409)
		} else {
			http.Error(w, "Cannot save upload; check storage", 507)
		}
		return
	}
	l.scan()
	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) playbackControl(w http.ResponseWriter, r *http.Request) {
	typ, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if !controlOriginAllowed(r) || typ != "application/json" {
		http.Error(w, "Control requests require same-origin JSON", 403)
		return
	}
	if s.library == nil || s.broadcast == nil {
		http.Error(w, "Enable BRB to play prepared videos", 409)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 2048)
	defer r.Body.Close()
	var body struct {
		Action   string  `json:"action"`
		ID       string  `json:"id"`
		Loop     bool    `json:"loop"`
		Position float64 `json:"position"`
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if dec.Decode(&body) != nil || dec.Decode(new(any)) != io.EOF {
		http.Error(w, "Expected one playback command", 400)
		return
	}
	if body.Action == "prepare" {
		if err := s.library.retry(body.ID); err != nil {
			http.Error(w, err.Error(), 409)
			return
		}
		w.WriteHeader(202)
		return
	}
	s.controlMu.Lock()
	defer s.controlMu.Unlock()
	b := s.broadcast
	if body.Action == "play" {
		if !s.forwarding.Load() {
			http.Error(w, "Turn master forwarding on before playing a video", 409)
			return
		}
		clip, profile, err := s.library.open(body.ID)
		if err != nil {
			http.Error(w, err.Error(), 409)
			return
		}
		b.mu.Lock()
		defer b.mu.Unlock()
		if profile != b.media.settings.Profile {
			clip.reader.close()
			http.Error(w, "Video needs preparation for the active profile", 409)
			return
		}
		b.stopClip("")
		clip.loop = body.Loop
		b.clip = clip
		b.live = false
		b.active = false
		w.WriteHeader(204)
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if body.Action == "stop" {
		b.stopClip("")
		w.WriteHeader(204)
		return
	}
	p := b.clip
	if p == nil {
		http.Error(w, "No video is selected for playback", 409)
		return
	}
	switch body.Action {
	case "pause":
		p.freeze(time.Now())
		p.paused = true
		b.active = false
		b.resetBroadcastPreview()
	case "resume":
		if b.manual {
			http.Error(w, "Turn manual BRB off to resume the video", 409)
			return
		}
		if p.paused {
			p.paused = false
			p.streaming = false
		}
	case "seek":
		if math.IsNaN(body.Position) || math.IsInf(body.Position, 0) || body.Position < 0 || body.Position >= p.reader.index.Duration.Seconds() {
			http.Error(w, "Seek position must be inside the video", 400)
			return
		}
		pos, err := p.reader.seek(time.Duration(body.Position * float64(time.Second)))
		if err != nil {
			http.Error(w, "Cannot seek this video", 409)
			return
		}
		p.continuation = false
		p.position = pos
		p.streaming = false
		p.pending = nil
		p.eof = false
		b.resetBroadcastPreview()
	default:
		http.Error(w, "Unknown playback command", 400)
		return
	}
	w.WriteHeader(204)
}
