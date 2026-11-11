package relay

import (
	"errors"
	"github.com/kreuzhofer/restreamer/internal/config"
	"net"
	"net/http"
	"os"
	"syscall"
	"time"
)

// This listener exposes only the status needed to refuse unsafe updates. Access
// requires the same OS identity as the private database (or host root). It never
// accepts dashboard commands and is never registered on the public HTTP mux.
func (s *Server) startAdminStatus() (func(), error) {
	database := s.cfg.UsersDatabasePath()
	if database == "" {
		return func() {}, nil
	}
	path, err := s.cfg.AdminSocketPath()
	if errors.Is(err, config.ErrAdminSocketPathTooLong) {
		s.log.Warn(err.Error())
		return func() {}, nil
	}
	if err != nil {
		return nil, err
	}
	directory := database + ".control"
	if err := os.MkdirAll(directory, 0700); err != nil {
		return nil, errors.New("cannot create private administration directory")
	}
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("administration directory must be private")
	}
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return nil, errors.New("administration path is not a socket")
		}
		connection, err := net.DialTimeout("unix", path, time.Second)
		if err == nil {
			connection.Close()
			return nil, errors.New("administration socket is already in use")
		}
		if !errors.Is(err, syscall.ECONNREFUSED) {
			return nil, errors.New("cannot verify stale administration socket")
		}
		if err = os.Remove(path); err != nil {
			return nil, errors.New("cannot remove stale administration socket")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, errors.New("cannot inspect administration socket")
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, errors.New("cannot bind private administration socket")
	}
	if err = os.Chmod(path, 0600); err != nil {
		listener.Close()
		return nil, errors.New("cannot protect administration socket")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /status", func(w http.ResponseWriter, r *http.Request) {
		stage := s.broadcast.stageStatus(time.Now())
		generatorJSON(w, 200, map[string]any{"stage": stage.Stage, "pending": stage.Pending != nil, "publisher_connected": s.active.Load()})
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 2 * time.Second, WriteTimeout: 2 * time.Second, IdleTimeout: 2 * time.Second}
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.log.Error("private administration listener stopped")
		}
	}()
	return func() { server.Close() }, nil
}
