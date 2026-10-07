// Package relay fans out a single live publisher to independent destinations.
package relay

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kreuzhofer/restreamer/internal/config"
	"github.com/kreuzhofer/restreamer/internal/rtmp"
)

type Server struct {
	cfg     config.Config
	log     *slog.Logger
	active  atomic.Bool
	outputs []*output
}

func New(cfg config.Config, log *slog.Logger) *Server {
	s := &Server{cfg: cfg, log: log}
	for _, target := range cfg.Targets {
		s.outputs = append(s.outputs, &output{config: target, log: log, status: OutputStatus{Name: target.Name, State: "idle"}})
	}
	return s
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("GET /status", func(w http.ResponseWriter, _ *http.Request) {
		status := struct {
			Publishing bool           `json:"publishing"`
			Outputs    []OutputStatus `json:"outputs"`
		}{Publishing: s.active.Load(), Outputs: make([]OutputStatus, 0, len(s.outputs))}
		for _, o := range s.outputs {
			status.Outputs = append(status.Outputs, o.snapshot())
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(status)
	})
	return mux
}

func (s *Server) Run(ctx context.Context) error {
	l, err := net.Listen("tcp", s.cfg.Listen)
	if err != nil {
		return errors.New("cannot bind RTMP listener")
	}
	defer l.Close()
	hl, err := net.Listen("tcp", s.cfg.HealthListen)
	if err != nil {
		return errors.New("cannot bind health listener")
	}
	httpServer := &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second}
	defer httpServer.Close()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	errHTTP := make(chan error, 1)
	go func() { errHTTP <- httpServer.Serve(hl); cancel() }()
	err = s.Serve(ctx, l)
	httpServer.Close()
	httpErr := <-errHTTP
	if httpErr != nil && !errors.Is(httpErr, http.ErrServerClosed) {
		return errors.New("health server stopped unexpectedly")
	}
	return err
}

// Serve owns l until all publishers, pending handshakes and output workers stop.
func (s *Server) Serve(ctx context.Context, l net.Listener) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(ctx, func() { l.Close() })
	defer stop()
	var wg sync.WaitGroup
	defer wg.Wait()
	defer cancel()
	slots := make(chan struct{}, 16)
	s.log.Info("RTMP listener ready", "address", l.Addr().String())
	for {
		n, err := l.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return errors.New("RTMP listener stopped unexpectedly")
		}
		select {
		case slots <- struct{}{}:
		default:
			n.Close()
			continue
		}
		wg.Add(1)
		go func() { defer wg.Done(); defer func() { <-slots }(); s.handle(ctx, n) }()
	}
}

func (s *Server) handle(ctx context.Context, n net.Conn) {
	defer n.Close()
	stop := context.AfterFunc(ctx, func() { n.Close() })
	defer stop()
	reserved := false
	defer func() {
		if reserved {
			s.active.Store(false)
		}
	}()
	expected := sha256.Sum256([]byte(s.cfg.StreamKey))
	c, err := rtmp.Accept(n, func(app, key string) bool {
		provided := sha256.Sum256([]byte(key))
		if app != s.cfg.Application || subtle.ConstantTimeCompare(expected[:], provided[:]) != 1 {
			return false
		}
		reserved = s.active.CompareAndSwap(false, true)
		return reserved
	})
	if err != nil {
		if ctx.Err() == nil {
			s.log.Warn("input connection rejected or handshake failed")
		}
		return
	}
	s.log.Info("publisher connected")
	defer s.log.Info("publisher disconnected")
	session, cancel := context.WithCancel(ctx)
	h := newHub(s.cfg.QueueBytes)
	var wg sync.WaitGroup
	defer wg.Wait()
	defer cancel()
	for _, o := range s.outputs {
		wg.Add(1)
		go func() { defer wg.Done(); o.run(session, h) }()
	}
	for {
		_ = n.SetReadDeadline(time.Now().Add(15 * time.Second))
		m, err := c.Read()
		if err != nil {
			return
		}
		if err := rtmp.CheckControl(m); err != nil {
			return
		}
		if err := h.publish(m); err != nil {
			s.log.Warn("input media rejected", "reason", err.Error())
			return
		}
	}
}
