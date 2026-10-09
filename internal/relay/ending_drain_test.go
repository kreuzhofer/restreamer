package relay

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/kreuzhofer/restreamer/internal/config"
	"github.com/kreuzhofer/restreamer/internal/rtmp"
)

func blockedWriteKeyframe(source *rtmp.Message, timestamp time.Duration) *rtmp.Message {
	frame := *source
	frame.Timestamp = timestamp
	frame.Body = append([]byte{}, source.Body...)
	// A valid filler NAL makes this genuine RTMP write exceed socket buffers.
	filler := bytes.Repeat([]byte{0xff}, 8<<20)
	filler[0], filler[len(filler)-1] = 12, 0x80
	prefix := make([]byte, 4)
	binary.BigEndian.PutUint32(prefix, uint32(len(filler)))
	frame.Body = append(frame.Body, prefix...)
	frame.Body = append(frame.Body, filler...)
	return &frame
}

type drainDestination struct {
	target                   config.Target
	ready, firstRead, closed <-chan struct{}
	readFirst, disconnect    func()
}

// This real RTMP sink initially reads no media. Reading exactly one frame later
// lets the relay start another blocked write, proving the ending deadline is
// independent of the ordinary timeout of whichever socket write is in flight.
func newDrainDestination(t *testing.T) drainDestination {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ready, firstRead, closed := make(chan struct{}), make(chan struct{}), make(chan struct{})
	advance := make(chan struct{})
	var once sync.Once
	readFirst := func() { once.Do(func() { close(advance) }) }
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		defer close(closed)
		n, err := listener.Accept()
		if err != nil {
			return
		}
		defer n.Close()
		stop := context.AfterFunc(ctx, func() { n.Close() })
		defer stop()
		tcp := n.(*net.TCPConn)
		tcp.SetReadBuffer(1024)
		c, err := rtmp.Accept(n, func(app, key string) bool { return app == "app" && key == "target-key" })
		if err != nil {
			return
		}
		close(ready)
		select {
		case <-advance:
		case <-ctx.Done():
			return
		}
		tcp.SetReadBuffer(1 << 20)
		for {
			message, err := c.Read()
			if err != nil {
				return
			}
			if isVideoFrame(message) {
				break
			}
		}
		tcp.SetReadBuffer(1024)
		close(firstRead)
		<-ctx.Done()
	}()
	t.Cleanup(func() { cancel(); listener.Close(); <-closed })
	return drainDestination{config.Target{Name: "stalled", URL: "rtmp://" + listener.Addr().String() + "/app", StreamKey: "target-key"}, ready, firstRead, closed, readFirst, cancel}
}

type drainMedia struct {
	index    *clipIndex
	keyframe *rtmp.Message
}

func startDrainRelay(t *testing.T, targets []config.Target) (*Server, string, drainMedia) {
	t.Helper()
	prepared, revision := preparedRevisionServer(t)
	if w := dashboardRequest(prepared, "PUT", "/api/stage-media", `{"prestream":"`+revision+`","ending":"`+revision+`","shortcuts":[]}`); w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	path := filepath.Join(prepared.library.root, "revisions", revision+".flv")
	index, err := indexClip(path)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := openClip(path, index)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.close()
	var keyframe *rtmp.Message
	for keyframe == nil {
		message, err := reader.next()
		if err != nil {
			t.Fatal(err)
		}
		if isVideoFrame(message) && message.Body[0]>>4 == 1 {
			keyframe = message
		}
	}
	cfg := prepared.cfg
	cfg.Application, cfg.StreamKey, cfg.QueueBytes, cfg.Targets = "live", "input-key-1234567890", 32<<20, targets
	s := New(cfg, prepared.log)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Serve(ctx, listener) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	})
	return s, "rtmp://" + listener.Addr().String() + "/live", drainMedia{index, keyframe}
}

func startDrainLive(t *testing.T, s *Server, address string, media drainMedia) *rtmp.Conn {
	t.Helper()
	if w := stageRequest(t, s, "go_live", nil); w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	publisher := publishInput(t, address)
	writePacket(t, publisher, media.index.Video)
	writePacket(t, publisher, media.index.Audio)
	writePacket(t, publisher, packet(rtmp.Audio, 0, 0xaf, 1, 0))
	writePacket(t, publisher, media.keyframe)
	eventually(t, func() bool {
		if readStage(t, s).Stage != "LIVE" {
			return false
		}
		for _, output := range readTargetDashboard(t, s).Outputs {
			if output.State != "waiting_for_keyframe" {
				return false
			}
		}
		return true
	})
	return publisher
}

func endingDestination(state StageStatus, name string) (EndingDestinationResult, bool) {
	for _, result := range state.Ending.Results {
		if result.Name == name {
			return result, true
		}
	}
	return EndingDestinationResult{}, false
}

func beginEndingDrain(t *testing.T, s *Server) time.Time {
	t.Helper()
	if w := stageRequest(t, s, "end_stream", nil); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	eventually(t, func() bool { state := readStage(t, s); return state.Ending.Draining || state.Ending.Completed })
	return time.Now()
}

func TestEndingDrainClosesHealthyDestinationWithoutWaitingForStalledWrite(t *testing.T) {
	healthy := newSink(t)
	stalled := newDrainDestination(t)
	s, address, media := startDrainRelay(t, []config.Target{healthy.target("healthy"), stalled.target})
	publisher := startDrainLive(t, s, address, media)
	<-stalled.ready
	first, second := blockedWriteKeyframe(media.keyframe, time.Second), blockedWriteKeyframe(media.keyframe, 2*time.Second)
	writePacket(t, publisher, first)
	writePacket(t, publisher, second)
	eventually(t, func() bool { return readTargetDashboard(t, s).InputFrames == 3 })
	drainStarted := beginEndingDrain(t, s)
	// Both already-admitted large packets reached the healthy peer unchanged.
	for _, want := range []*rtmp.Message{first, second} {
		for {
			got := receive(t, healthy)
			if !isVideoFrame(got) || len(got.Body) < 8<<20 {
				continue
			}
			if !bytes.Equal(got.Body, want.Body) {
				t.Fatal("queued packet changed during ending")
			}
			break
		}
	}
	eventually(t, func() bool {
		result, done := endingDestination(readStage(t, s), "healthy")
		if done && (!result.Complete || result.Reason != "") {
			t.Fatal("healthy completion", result)
		}
		return done
	})
	eventually(t, func() bool {
		healthy.mu.Lock()
		defer healthy.mu.Unlock()
		return healthy.conn != nil && errors.Is(healthy.conn.SetReadDeadline(time.Time{}), net.ErrClosed)
	})
	if _, done := endingDestination(readStage(t, s), "stalled"); done {
		t.Fatal("stalled in-flight writes were reported complete with healthy output")
	}
	if time.Since(drainStarted) > 2*time.Second {
		t.Fatal("healthy output waited for stalled output")
	}
	// Restart an ordinary socket-write timeout after EOF. The absolute ending
	// deadline must still interrupt this second blocked write at EOF + 10s.
	time.Sleep(time.Second)
	stalled.readFirst()
	select {
	case <-stalled.firstRead:
	case <-time.After(3 * time.Second):
		t.Fatal("stalled fixture did not consume its first frame")
	}
	deadline := time.NewTimer(11*time.Second - time.Since(drainStarted))
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for !readStage(t, s).Ending.Completed {
		select {
		case <-ticker.C:
		case <-deadline.C:
			t.Fatal("ending exceeded its independent 10-second bound")
		}
	}
	elapsed := time.Since(drainStarted)
	result, done := endingDestination(readStage(t, s), "stalled")
	if !done || result.Complete || result.Reason != "deadline_exceeded" {
		t.Fatal("stalled completion", result)
	}
	if elapsed < 9*time.Second || elapsed > 11*time.Second {
		t.Fatal("ending deadline moved", elapsed)
	}
	// Further OBS media cannot start another destination session after ending.
	publisher = publishInput(t, address)
	writePacket(t, publisher, media.index.Video)
	writePacket(t, publisher, media.index.Audio)
	writePacket(t, publisher, packet(rtmp.Audio, 0, 0xaf, 1, 0))
	writePacket(t, publisher, media.keyframe)
	eventually(t, func() bool { return readTargetDashboard(t, s).InputFrames == 4 })
	if readStage(t, s).Stage != "OFF" || healthy.count() != 1 {
		t.Fatal("post-ending input restarted delivery")
	}
}

func TestEndingDrainCancelsPendingHandshakeAndDoesNotReconnect(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	pending := config.Target{Name: "pending", URL: "rtmp://" + listener.Addr().String() + "/app", StreamKey: "target-key"}
	disabled := pending
	disabled.Name, disabled.Enabled = "disabled", "false"
	stalled := newDrainDestination(t)
	s, address, media := startDrainRelay(t, []config.Target{pending, disabled, stalled.target})
	if w := stageRequest(t, s, "go_live", nil); w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	publisher := publishInput(t, address)
	writePacket(t, publisher, media.index.Video)
	writePacket(t, publisher, media.index.Audio)
	writePacket(t, publisher, packet(rtmp.Audio, 0, 0xaf, 1, 0))
	writePacket(t, publisher, media.keyframe)
	eventually(t, func() bool {
		for _, output := range readTargetDashboard(t, s).Outputs {
			if output.Name == "stalled" {
				return output.State == "waiting_for_keyframe"
			}
		}
		return false
	})
	listener.(*net.TCPListener).SetDeadline(time.Now().Add(3 * time.Second))
	n, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer n.Close()
	closed := make(chan struct{})
	go func() { io.Copy(io.Discard, n); close(closed) }()
	writePacket(t, publisher, blockedWriteKeyframe(media.keyframe, time.Second))
	eventually(t, func() bool { return readTargetDashboard(t, s).InputFrames == 2 })
	beginEndingDrain(t, s)
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("ending left publish handshake pending")
	}
	state := readStage(t, s)
	if !state.Ending.Draining {
		t.Fatal("fixture did not retain an active ending drain", state)
	}
	if result, done := endingDestination(state, "pending"); !done || result.Complete || result.Reason != "unavailable" {
		t.Fatal("unavailable result", result)
	}
	if result, done := endingDestination(state, "disabled"); !done || !result.Complete || result.Reason != "disabled" {
		t.Fatal("disabled result", result)
	}
	listener.(*net.TCPListener).SetDeadline(time.Now().Add(1500 * time.Millisecond))
	if unexpected, err := listener.Accept(); err == nil {
		unexpected.Close()
		t.Fatal("destination reconnected after ending")
	} else if networkError, ok := err.(net.Error); !ok || !networkError.Timeout() {
		t.Fatal(err)
	}
	if !readStage(t, s).Ending.Draining {
		t.Fatal("pending-connection observation did not occur during final drain")
	}
	if w := stageRequest(t, s, "stop_now", nil); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	// Starting a new confirmed show clears the previous ending admission gate.
	if w := stageRequest(t, s, "prestream", nil); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	listener.(*net.TCPListener).SetDeadline(time.Now().Add(3 * time.Second))
	next, err := listener.Accept()
	if err != nil {
		t.Fatalf("fresh show could not connect: %v", err)
	}
	next.Close()
}

func TestEndingDrainReportsPeerFailureBeforeDeadline(t *testing.T) {
	stalled := newDrainDestination(t)
	s, address, media := startDrainRelay(t, []config.Target{stalled.target})
	publisher := startDrainLive(t, s, address, media)
	<-stalled.ready
	writePacket(t, publisher, blockedWriteKeyframe(media.keyframe, time.Second))
	eventually(t, func() bool { return readTargetDashboard(t, s).InputFrames == 2 })
	beginEndingDrain(t, s)
	stalled.disconnect()
	eventually(t, func() bool { return readStage(t, s).Ending.Completed })
	if result, done := endingDestination(readStage(t, s), "stalled"); !done || result.Complete || result.Reason != "connection_closed_or_write_failed" {
		t.Fatal("peer failure completion", result)
	}
}
