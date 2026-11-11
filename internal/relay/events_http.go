package relay

import (
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func (s *Server) eventViewerHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; media-src 'self' blob:; worker-src blob:; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
	if err := s.initialize(); err != nil {
		http.Error(w, "Event service unavailable", 503)
		return
	}
	e := s.event(r.PathValue("id"))
	if e == nil {
		http.NotFound(w, r)
		return
	}
	e.mu.Lock()
	if e.State == "deleted" || e.unavailable {
		e.mu.Unlock()
		http.NotFound(w, r)
		return
	}
	resource := r.PathValue("resource")
	if resource == "login" && r.Method == "POST" {
		e.mu.Unlock()
		e.login(w, r, s.eventAuthSlots)
		return
	}
	if resource != "" && !e.authorized(r) {
		e.mu.Unlock()
		http.Error(w, "Enter the event password", 401)
		return
	}
	if resource == "status" {
		view := e.view()
		e.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(view)
		return
	}
	if resource != "" {
		s.eventMediaHTTP(w, r, e, resource, false)
		return
	}
	e.mu.Unlock()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	data, err := dashboardFiles.ReadFile("web/event.html")
	if err != nil {
		http.Error(w, "Viewer unavailable", 503)
		return
	}
	w.Write(data)
}

// Called with e.mu held; releases it before writing to any viewer socket.
func (s *Server) eventMediaHTTP(w http.ResponseWriter, r *http.Request, e *streamingEvent, resource string, operator bool) {
	replay := operator || e.Published && (e.State == "ended" || e.State == "interrupted" || e.State == "failed")
	live := e.State == "live" && e.delivering.Load()
	if resource == "live.m3u8" || resource == "replay.m3u8" {
		allowed := live
		if resource == "replay.m3u8" {
			allowed = replay
		}
		if !allowed || len(e.Segments) == 0 {
			e.mu.Unlock()
			http.NotFound(w, r)
			return
		}
		playlist := e.playlist(resource == "replay.m3u8")
		e.mu.Unlock()
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		w.Write([]byte(playlist))
		return
	}
	segments := e.Segments
	if !replay && len(segments) > eventLiveSegments {
		segments = segments[len(segments)-eventLiveSegments:]
	}
	allowed := false
	if replay || live {
		for _, segment := range segments {
			if resource == segment.Name || resource == segment.Init {
				allowed = true
				break
			}
		}
	}
	if !allowed {
		e.mu.Unlock()
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(filepath.Join(e.dir, resource))
	e.mu.Unlock()
	if err != nil {
		http.Error(w, "Recording media unavailable", 503)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		http.Error(w, "Recording media unavailable", 503)
		return
	}
	select {
	case s.eventSlots <- struct{}{}:
		defer func() { <-s.eventSlots }()
	default:
		http.Error(w, "Viewer capacity reached; try again shortly", 429)
		return
	}
	w.Header().Set("Content-Type", "video/mp4")
	http.ServeContent(w, r, resource, info.ModTime(), f)
}

func (e *streamingEvent) authorized(r *http.Request) bool {
	if len(e.Hash) == 0 {
		return true
	}
	c, err := r.Cookie("event_session")
	if err != nil {
		return false
	}
	parts := strings.Split(c.Value, ".")
	if len(parts) != 2 {
		return false
	}
	expires, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || time.Now().Unix() >= expires {
		return false
	}
	signature, err := hex.DecodeString(parts[1])
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, e.SessionKey)
	mac.Write([]byte(e.ID + ":" + parts[0]))
	return hmac.Equal(signature, mac.Sum(nil))
}

func eventJSON(w http.ResponseWriter, r *http.Request, value any) bool {
	if !controlOriginAllowed(r) || !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		http.Error(w, "Changes require same-origin JSON", 403)
		return false
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
	d.DisallowUnknownFields()
	if d.Decode(value) != nil || d.Decode(new(any)) != io.EOF {
		http.Error(w, "Expected one JSON request", 400)
		return false
	}
	return true
}

func (e *streamingEvent) login(w http.ResponseWriter, r *http.Request, slots chan struct{}) {
	var input struct{ Password string }
	if !eventJSON(w, r, &input) {
		return
	}
	if len(input.Password) > 256 {
		http.Error(w, "Invalid password", 401)
		return
	}
	select {
	case slots <- struct{}{}:
		defer func() { <-slots }()
	default:
		http.Error(w, "Sign-in is busy; try again shortly", 429)
		return
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	e.mu.Lock()
	if e.loginAttempts == nil {
		e.loginAttempts = make(map[string]eventLoginAttempts)
	}
	now := time.Now()
	for key, attempts := range e.loginAttempts {
		if now.Sub(attempts.Since) > time.Minute {
			delete(e.loginAttempts, key)
		}
	}
	attempts := e.loginAttempts[host]
	if attempts.Count >= 10 || len(e.loginAttempts) >= 256 && attempts.Count == 0 {
		e.mu.Unlock()
		w.Header().Set("Retry-After", "60")
		http.Error(w, "Too many attempts; try again in a minute", 429)
		return
	}
	if attempts.Count == 0 {
		attempts.Since = now
	}
	attempts.Count++
	e.loginAttempts[host] = attempts
	salt, expected, key := append([]byte{}, e.Salt...), append([]byte{}, e.Hash...), append([]byte{}, e.SessionKey...)
	e.mu.Unlock()
	hash, err := pbkdf2.Key(sha256.New, input.Password, salt, 600000, 32)
	if len(expected) > 0 && (err != nil || subtle.ConstantTimeCompare(hash, expected) != 1) {
		http.Error(w, "Invalid password", 401)
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.State == "deleted" || e.unavailable || !hmac.Equal(key, e.SessionKey) {
		http.Error(w, "Event access changed; sign in again", 401)
		return
	}
	delete(e.loginAttempts, host)
	expires := strconv.FormatInt(time.Now().Add(12*time.Hour).Unix(), 10)
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(e.ID + ":" + expires))
	http.SetCookie(w, &http.Cookie{Name: "event_session", Value: expires + "." + hex.EncodeToString(mac.Sum(nil)), Path: "/watch/" + e.ID + "/", HttpOnly: true, Secure: r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https", SameSite: http.SameSiteStrictMode, MaxAge: 12 * 3600})
	w.WriteHeader(204)
}

func (s *Server) eventsHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		s.eventsMu.RLock()
		list := make([]*streamingEvent, 0, len(s.events))
		for _, e := range s.events {
			list = append(list, e)
		}
		s.eventsMu.RUnlock()
		views := []map[string]any{}
		for _, e := range list {
			e.mu.Lock()
			views = append(views, e.view())
			e.mu.Unlock()
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"events": views, "available": s.cfg.EventsDirectory != "" && s.eventStorageError == "", "error": s.eventStorageError})
		return
	}
	var change struct {
		Version               uint64  `json:"version"`
		Password              *string `json:"password,omitempty"`
		Published             *bool   `json:"published,omitempty"`
		AcknowledgeIncomplete bool    `json:"acknowledge_incomplete,omitempty"`
	}
	if !eventJSON(w, r, &change) {
		return
	}
	e := s.event(r.PathValue("id"))
	if e == nil {
		http.NotFound(w, r)
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.Version != change.Version {
		http.Error(w, "Event changed; refresh before saving", 409)
		return
	}
	oldSalt, oldHash, oldKey := e.Salt, e.Hash, e.SessionKey
	oldPublished := e.Published
	if e.State == "deleted" || e.unavailable {
		http.NotFound(w, r)
		return
	}
	if change.Published != nil {
		if *change.Published && (len(e.Segments) == 0 || e.State == "live" || e.State == "waiting" || e.Incomplete && !change.AcknowledgeIncomplete) {
			http.Error(w, "A finalized recording is required; acknowledge an incomplete recording before publishing", 409)
			return
		}
		if *change.Published {
			version := e.Version
			if err := e.recoverRecording(); err != nil {
				e.Published, e.Incomplete, e.unavailable = false, true, true
				e.Error = err.Error()
				http.Error(w, "Recording validation failed; restore storage before publishing", 503)
				return
			}
			if e.Version != version {
				http.Error(w, "Recording media changed; review recovered media before publishing", 409)
				return
			}
		}
		e.Published = *change.Published
	}
	if change.Password != nil {
		if err := e.setPassword(*change.Password); err != nil {
			e.Published = oldPublished
			http.Error(w, err.Error(), 400)
			return
		}
	}
	e.Version++
	if err := e.save(); err != nil {
		e.Salt, e.Hash, e.SessionKey = oldSalt, oldHash, oldKey
		e.Published = oldPublished
		e.Version--
		http.Error(w, "Cannot save event; previous settings retained", 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(e.view())
}
