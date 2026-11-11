package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image/jpeg"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var eventThumbnailName = regexp.MustCompile(`^thumbnail-([0-9]{8})\.jpg$`)

// Called with e.mu held. Images are derived only from indexed, immutable media;
// the caller has already checked the event password and replay publication.
func (s *Server) eventThumbnailHTTP(w http.ResponseWriter, r *http.Request, e *streamingEvent, resource string, allowed bool) {
	if !allowed || len(e.Segments) == 0 {
		e.mu.Unlock()
		http.NotFound(w, r)
		return
	}
	if resource == "thumbnails.json" {
		type frame struct {
			Time     float64 `json:"time"`
			Resource string  `json:"resource"`
		}
		frames := make([]frame, 0, len(e.Segments))
		duration := 0.0
		for _, segment := range e.Segments {
			frames = append(frames, frame{duration, "thumbnail-" + strings.TrimSuffix(strings.TrimPrefix(segment.Name, "segment-"), ".m4s") + ".jpg"})
			duration += segment.Duration
		}
		e.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(struct {
			Duration float64 `json:"duration"`
			Poster   string  `json:"poster"`
			Frames   []frame `json:"frames"`
		}{duration, "poster.jpg", frames})
		return
	}
	select {
	case s.eventSlots <- struct{}{}:
		defer func() { <-s.eventSlots }()
	default:
		e.mu.Unlock()
		http.Error(w, "Viewer capacity reached; try again shortly", 429)
		return
	}
	index, width, height, limit := 0, 192, 108, int64(128<<10)
	cacheName := resource
	if resource == "poster.jpg" {
		width, height, limit = 1280, 720, 1<<20
		cacheName = "poster-" + e.Segments[0].Checksum + ".jpg"
	} else {
		match := eventThumbnailName.FindStringSubmatch(resource)
		if match == nil {
			e.mu.Unlock()
			http.NotFound(w, r)
			return
		}
		index = -1
		for i, segment := range e.Segments {
			if segment.Name == "segment-"+match[1]+".m4s" {
				index = i
				break
			}
		}
	}
	if index < 0 || index >= len(e.Segments) {
		e.mu.Unlock()
		http.NotFound(w, r)
		return
	}
	path := filepath.Join(e.dir, cacheName)
	if f, err := os.Open(path); err == nil {
		e.mu.Unlock()
		defer f.Close()
		info, err := f.Stat()
		if err != nil || !info.Mode().IsRegular() || info.Size() > limit {
			http.Error(w, "Replay preview is unavailable", 503)
			return
		}
		w.Header().Set("Content-Type", "image/jpeg")
		http.ServeContent(w, r, resource, info.ModTime(), f)
		return
	} else if !os.IsNotExist(err) {
		e.mu.Unlock()
		http.Error(w, "Cannot read replay preview", 503)
		return
	}
	select {
	case s.eventThumbnailSlots <- struct{}{}:
		defer func() { <-s.eventThumbnailSlots }()
	default:
		e.mu.Unlock()
		w.Header().Set("Retry-After", "1")
		http.Error(w, "Replay preview is busy; try again shortly", 429)
		return
	}
	segment := e.Segments[index]
	init, err := os.Open(filepath.Join(e.dir, segment.Init))
	if err != nil {
		e.mu.Unlock()
		http.Error(w, "Recording media unavailable", 503)
		return
	}
	defer init.Close()
	media, err := os.Open(filepath.Join(e.dir, segment.Name))
	e.mu.Unlock()
	if err != nil {
		http.Error(w, "Recording media unavailable", 503)
		return
	}
	defer media.Close()
	// A bounded first-frame decode avoids scanning a whole recording. Only one
	// request can decode at a time; disconnects cancel FFmpeg and release the slot.
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(10 * time.Second))
	args := []string{"-v", "error", "-nostdin", "-max_alloc", "268435456", "-threads", "1", "-protocol_whitelist", "pipe", "-f", "mp4", "-enable_drefs", "0", "-use_absolute_path", "0", "-i", "pipe:0", "-an", "-sn", "-dn", "-frames:v", "1", "-filter_threads", "1", "-vf", fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=decrease,pad=%d:%d:(ow-iw)/2:(oh-ih)/2", width, height, width, height), "-threads", "1", "-c:v", "mjpeg", "-q:v", "4", "-f", "image2pipe", "pipe:1"}
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	cmd.Stdin = io.MultiReader(init, media)
	var data bytes.Buffer
	cmd.Stdout = &assetLimitWriter{Writer: &data, remaining: limit}
	if cmd.Run() != nil {
		http.Error(w, "Cannot create replay preview; check recording media and FFmpeg", 503)
		return
	}
	config, err := jpeg.DecodeConfig(bytes.NewReader(data.Bytes()))
	if err != nil || config.Width != width || config.Height != height {
		http.Error(w, "Cannot decode replay preview", 503)
		return
	}
	e.mu.Lock()
	// Deletion must never be undone by a late cache write.
	if e.State == "deleted" || e.unavailable {
		e.mu.Unlock()
		http.NotFound(w, r)
		return
	}
	err = writeEventMedia(path, data.Bytes())
	e.mu.Unlock()
	if err != nil {
		http.Error(w, "Cannot save replay preview; check event storage", 503)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Write(data.Bytes())
}
