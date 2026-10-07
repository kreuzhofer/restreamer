// Package relay fans out a single live publisher to independent destinations.
package relay

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
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
	cfg        config.Config
	log        *slog.Logger
	active     atomic.Bool
	outputs    []*output
	inputBytes atomic.Uint64
	metrics    metrics
	controlMu  sync.Mutex
	initOnce   sync.Once
	initErr    error
}

func New(cfg config.Config, log *slog.Logger) *Server {
	s := &Server{cfg: cfg, log: log}
	for _, target := range cfg.Targets {
		state := "idle"
		if !target.IsEnabled() {
			state = "disabled"
		}
		status := OutputStatus{Name: target.Name, Enabled: target.IsEnabled(), State: state, CanEnable: target.ReadyError() == nil}
		if err := target.ReadyError(); err != nil {
			status.UnavailableReason = err.Error()
		}
		s.outputs = append(s.outputs, &output{config: target, log: log, changed: make(chan struct{}, 1), status: status})
	}
	return s
}

func (s *Server) Run(ctx context.Context) error {
	if err := s.initialize(); err != nil {
		return err
	}
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
	defer l.Close()
	if err := s.initialize(); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(ctx, func() { l.Close() })
	defer stop()
	var wg sync.WaitGroup
	defer wg.Wait()
	defer cancel()
	wg.Add(1)
	go func() { defer wg.Done(); s.sampleLoop(ctx) }()
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
	authorizationReason := ""
	c, err := rtmp.Accept(n, func(app, key string) bool {
		provided := sha256.Sum256([]byte(key))
		if app != s.cfg.Application {
			authorizationReason = "application_mismatch"
			return false
		}
		if subtle.ConstantTimeCompare(expected[:], provided[:]) != 1 {
			authorizationReason = "stream_key_mismatch"
			return false
		}
		reserved = s.active.CompareAndSwap(false, true)
		if !reserved {
			authorizationReason = "publisher_already_connected"
		}
		return reserved
	})
	if err != nil {
		if ctx.Err() == nil {
			stage, reason := "unknown", "protocol_error"
			var acceptErr *rtmp.AcceptError
			if errors.As(err, &acceptErr) {
				stage, reason = acceptErr.Stage, acceptErr.Reason
			}
			if authorizationReason != "" {
				reason = authorizationReason
			}
			hint := "Use rtmp:// for OBS input and check the OBS connection log"
			switch reason {
			case "application_mismatch":
				hint = "OBS Server must end with /" + s.cfg.Application + "; enter the stream key separately"
			case "stream_key_mismatch":
				hint = "OBS Stream Key must match INGEST_STREAM_KEY in the running container"
			case "publisher_already_connected":
				hint = "Stop the existing publisher or wait for its connection to expire"
			}
			s.log.Warn("input connection failed", "stage", stage, "reason", reason, "hint", hint)
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
		go func() { defer wg.Done(); o.manage(session, h) }()
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
		if m.Type == rtmp.Video || m.Type == rtmp.Audio {
			s.inputBytes.Add(uint64(len(m.Body)))
		}
	}
}
