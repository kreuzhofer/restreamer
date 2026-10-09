package relay

import (
	"encoding/json"
	"errors"
	"io"
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
