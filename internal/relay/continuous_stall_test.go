package relay

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/kreuzhofer/restreamer/internal/config"
	"github.com/kreuzhofer/restreamer/internal/rtmp"
)

func TestContinuousBriefEncoderStallsPreserveSessionAndCatchUp(t *testing.T) {
	requireFFmpeg(t)
	realFFmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "encoder.pid")
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
	wrapper := fmt.Sprintf("#!/bin/sh\ncase \" $* \" in *' -video_size '*) printf '%%s\\n' \"$$\" > %s;; esac\nexec %s \"$@\"\n", quote(pidFile), quote(realFFmpeg))
	if err := os.WriteFile(filepath.Join(dir, "ffmpeg"), []byte(wrapper), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	dest := newSink(t)
	target := dest.target("continuous")
	target.OutputMode = "continuous"
	s, address := startRelayWithLogger(t, []config.Target{target}, slog.New(slog.NewTextHandler(io.Discard, nil)), true)
	if w := stageRequest(t, s, "go_live", nil); w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	ctx, cancel := context.WithCancel(context.Background())
	publisher := exec.CommandContext(ctx, realFFmpeg, "-v", "error", "-nostdin", "-re", "-f", "lavfi", "-i", "color=red:size=320x180:rate=25", "-f", "lavfi", "-i", "aevalsrc=if(lt(mod(t\\,1)\\,0.04)\\,0.7*sin(2*PI*2000*t)\\,0)|0.1*sin(2*PI*880*t):s=48000", "-vf", "drawbox=x=0:y=0:w=32:h=32:color=white:t=fill:enable='eq(mod(n,25),0)'", "-c:v", "libx264", "-threads", "2", "-g", "25", "-bf", "2", "-c:a", "aac", "-ar", "48000", "-ac", "2", "-f", "flv", address+"/input-key-1234567890")
	if err := publisher.Start(); err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); _ = publisher.Wait() })
	var frameCount atomic.Int64
	var cadenceBroke atomic.Bool
	captureCtx, stopCapture := context.WithCancel(context.Background())
	defer stopCapture()
	captured := make(chan []*rtmp.Message, 1)
	go func() {
		var packets []*rtmp.Message
		var last time.Duration
		for {
			select {
			case <-captureCtx.Done():
				captured <- packets
				return
			case m := <-dest.packets:
				packets = append(packets, m)
				if isVideoFrame(m) {
					if frameCount.Load() > 0 && m.Timestamp-last != 40*time.Millisecond {
						cadenceBroke.Store(true)
					}
					last = m.Timestamp
					frameCount.Add(1)
				}
			}
		}
	}()
	deadline := time.Now().Add(12 * time.Second)
	for readStage(t, s).Continuous.Source != "obs" {
		if time.Now().After(deadline) {
			t.Fatal("OBS not ready", readStage(t, s).Continuous)
		}
		time.Sleep(20 * time.Millisecond)
	}
	raw, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	encoder, err := os.FindProcess(pid)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = encoder.Signal(syscall.SIGCONT) })
	for _, pause := range []time.Duration{time.Second, 2 * time.Second} {
		before, started := frameCount.Load(), time.Now()
		if err := encoder.Signal(syscall.SIGSTOP); err != nil {
			t.Fatal(err)
		}
		time.Sleep(pause)
		_ = encoder.Signal(syscall.SIGCONT)
		deadline = time.Now().Add(5 * time.Second)
		for {
			state := readStage(t, s).Continuous
			if state.Restarts != 0 || dest.count() != 1 {
				t.Fatalf("%v stall restarted a healthy encoder: %+v connections=%d", pause, state, dest.count())
			}
			// Recover the elapsed media timeline, rather than retaining another
			// pause's worth of delay on every transient scheduling stall.
			expected := int64(time.Since(started) / (40 * time.Millisecond))
			if frameCount.Load()-before >= expected-3 && state.Source == "obs" {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("encoder did not catch up: emitted=%d expected=%d", frameCount.Load()-before, expected)
			}
			time.Sleep(20 * time.Millisecond)
		}
		if state := readStage(t, s).Continuous; state.DelayMS == nil || *state.DelayMS > 8000 {
			t.Fatal("unbounded recovery delay", state)
		}
	}
	time.Sleep(time.Second)
	// Stop must discard a queued backlog even when the encoder cannot read it.
	if err := encoder.Signal(syscall.SIGSTOP); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	started := time.Now()
	if w := stageRequest(t, s, "stop_now", nil); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	for readStage(t, s).Continuous.State != "off" {
		if time.Since(started) > time.Second {
			t.Fatal("Stop waited for stalled encoder backlog")
		}
		time.Sleep(10 * time.Millisecond)
	}
	stopCapture()
	packets := <-captured
	if cadenceBroke.Load() || dest.count() != 1 {
		t.Fatal("output cadence/session changed", dest.count())
	}
	path := filepath.Join(t.TempDir(), "stalled.flv")
	writeTestFLV(t, path, packets)
	if out, err := exec.Command(realFFmpeg, "-v", "error", "-xerror", "-i", path, "-f", "null", "-").CombinedOutput(); err != nil {
		t.Fatalf("decode failed: %v %s", err, out)
	}
	pixels, err := exec.Command("ffmpeg", "-v", "error", "-i", path, "-vf", "crop=2:2:40:40,scale=1:1,format=gray", "-fps_mode", "passthrough", "-f", "rawvideo", "pipe:1").Output()
	if err != nil {
		t.Fatal(err)
	}
	var frameTimes []float64
	for _, m := range packets {
		if isVideoFrame(m) {
			frameTimes = append(frameTimes, m.Timestamp.Seconds())
		}
	}
	if len(pixels) != len(frameTimes) {
		t.Fatalf("decoded %d of %d frames", len(pixels), len(frameTimes))
	}
	audio, err := exec.Command("ffmpeg", "-v", "error", "-copyts", "-i", path, "-vn", "-af", "pan=mono|c0=c0,aresample=48000:first_pts=0", "-f", "s16le", "pipe:1").Output()
	if err != nil {
		t.Fatal(err)
	}
	var pulses []float64
	high := false
	for offset := 0; offset+960 <= len(audio); offset += 960 {
		power := 0.0
		for j := offset; j < offset+960; j += 2 {
			v := float64(int16(binary.LittleEndian.Uint16(audio[j:j+2]))) / 32768
			power += v * v
		}
		loud := math.Sqrt(power/480) > 0.15
		if loud && !high {
			pulses = append(pulses, float64(offset)/96000)
		}
		high = loud
	}
	matched := 0
	for i, pixel := range pixels {
		if pixel < 220 || i > 0 && pixels[i-1] >= 220 {
			continue
		}
		at := frameTimes[i]
		if at < 3 {
			continue
		}
		nearest := math.Inf(1)
		for _, pulse := range pulses {
			nearest = math.Min(nearest, math.Abs(pulse-at))
		}
		if nearest > 0.080001 { // Allow floating-point rounding at exactly 80 ms.
			t.Fatalf("flash at %.3fs has nearest audio marker %.3fs away (pulses %v)", at, nearest, pulses)
		}
		matched++
	}
	if matched < 5 {
		t.Fatalf("too few synchronized markers: %d (pulses %v)", matched, pulses)
	}
}
