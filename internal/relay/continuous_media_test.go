package relay

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/kreuzhofer/restreamer/internal/config"
	"github.com/kreuzhofer/restreamer/internal/rtmp"
)

func TestContinuousOutputKeepsEncodingWhenPublisherDisconnects(t *testing.T) {
	requireFFmpeg(t)
	dest := newSink(t)
	target := dest.target("continuous")
	target.OutputMode = "continuous"
	s, address := startRelayWithLogger(t, []config.Target{target}, slog.New(slog.NewTextHandler(io.Discard, nil)), true)
	if w := stageRequest(t, s, "go_live", nil); w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var log bytes.Buffer
	publisher := exec.CommandContext(ctx, "ffmpeg", "-v", "error", "-nostdin", "-re", "-f", "lavfi", "-i", "testsrc2=size=320x180:rate=25", "-f", "lavfi", "-i", "sine=frequency=880:sample_rate=48000", "-t", "7", "-vf", "drawbox=x=100:y=50:w=100:h=100:color=red:t=fill", "-c:v", "libx264", "-threads", "2", "-pix_fmt", "yuv420p", "-g", "25", "-bf", "0", "-c:a", "aac", "-ar", "48000", "-ac", "2", "-f", "flv", address+"/input-key-1234567890")
	publisher.Stderr = &log
	if err := publisher.Start(); err != nil {
		t.Fatal(err)
	}
	finished := make(chan error, 1)
	go func() { finished <- publisher.Wait() }()
	var packets []*rtmp.Message
	deadline := time.NewTimer(13 * time.Second)
	defer deadline.Stop()
capture:
	for {
		select {
		case m := <-dest.packets:
			packets = append(packets, m)
		case <-deadline.C:
			break capture
		}
	}
	cancel()
	if err := <-finished; err != nil {
		t.Fatalf("publisher: %v %s", err, log.String())
	}
	if w := stageRequest(t, s, "stop_now", nil); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	frames := 0
	var last time.Duration
	for _, m := range packets {
		if !isVideoFrame(m) {
			continue
		}
		if frames > 0 && m.Timestamp-last != 40*time.Millisecond {
			t.Fatalf("continuous cadence broke: %v -> %v", last, m.Timestamp)
		}
		last = m.Timestamp
		frames++
	}
	if frames < 250 || dest.count() != 1 {
		t.Fatalf("output stopped with input or reconnected: frames=%d connections=%d", frames, dest.count())
	}
	path := filepath.Join(t.TempDir(), "continuous.flv")
	writeTestFLV(t, path, packets)
	if out, err := exec.Command("ffmpeg", "-v", "error", "-xerror", "-i", path, "-f", "null", "-").CombinedOutput(); err != nil {
		t.Fatalf("output decode: %v %s", err, out)
	}
	pixels, err := exec.Command("ffmpeg", "-v", "error", "-i", path, "-vf", "crop=2:2:960:540,scale=1:1", "-pix_fmt", "rgb24", "-f", "rawvideo", "pipe:1").Output()
	firstRed, redFrames := -1, 0
	for i := 0; i+2 < len(pixels); i += 3 {
		if pixels[i] > 180 && pixels[i+1] < 60 && pixels[i+2] < 60 {
			if firstRed < 0 {
				firstRed = i / 3
			}
			redFrames++
		}
	}
	if err != nil || firstRed < 0 || firstRed > 8*25 || redFrames < 25 {
		t.Fatalf("live picture missing or exceeds 8s delay: first frame=%d red frames=%d error=%v", firstRed, redFrames, err)
	}
	metadata, err := exec.Command("ffprobe", "-v", "error", "-select_streams", "v:0", "-show_entries", "stream=width,height,r_frame_rate", "-of", "json", path).Output()
	if err != nil {
		t.Fatal(err)
	}
	var probe struct {
		Streams []struct {
			Width, Height int
			Rate          string `json:"r_frame_rate"`
		}
	}
	if json.Unmarshal(metadata, &probe) != nil || len(probe.Streams) != 1 || probe.Streams[0].Width != 1920 || probe.Streams[0].Height != 1080 || probe.Streams[0].Rate != "25/1" {
		t.Fatalf("continuous profile: %s", metadata)
	}
}

func TestContinuousFixedEndingDeliversItsFinalFrameBeforeStopping(t *testing.T) {
	requireFFmpeg(t)
	prepared := libraryServer(t)
	file := filepath.Join(t.TempDir(), "ending.mp4")
	if out, err := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "color=red:size=320x180:rate=25", "-f", "lavfi", "-i", "sine=frequency=880:sample_rate=48000", "-t", "2", "-vf", "drawbox=color=lime:t=fill:enable='gte(t,1.96)'", "-c:v", "libx264", "-threads", "2", "-g", "25", "-bf", "0", "-c:a", "aac", "-ac", "2", file).CombinedOutput(); err != nil {
		t.Fatalf("fixture: %v %s", err, out)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	prepCtx, stopPrep := context.WithCancel(context.Background())
	prepDone := make(chan struct{})
	go func() { defer close(prepDone); prepared.library.worker(prepCtx) }()
	t.Cleanup(func() { stopPrep(); <-prepDone })
	if w := videoUpload(t, prepared, "ending.mp4", data); w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	prepared.library.scan()
	revision := readyHTTPRevision(t, prepared)
	if w := dashboardRequest(prepared, "PUT", "/api/stage-media", `{"prestream":"`+revision+`","ending":"`+revision+`","shortcuts":[]}`); w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	dest := newSink(t)
	target := dest.target("continuous")
	target.OutputMode = "continuous"
	cfg := prepared.cfg
	cfg.Targets = []config.Target{target}
	s := New(cfg, prepared.log)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Serve(ctx, l) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	})
	if w := stageRequest(t, s, "prestream", nil); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	eventually(t, func() bool { return readStage(t, s).Continuous.Source == "file" && dest.count() == 1 })
	if w := stageRequest(t, s, "end_stream", nil); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	deadline := time.NewTimer(12 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	var packets []*rtmp.Message
collect:
	for {
		select {
		case m := <-dest.packets:
			packets = append(packets, m)
		case <-tick.C:
			if readStage(t, s).Stage == "OFF" {
				break collect
			}
		case <-deadline.C:
			t.Fatal("ending did not finish", readStage(t, s))
		}
	}
	for len(dest.packets) > 0 {
		packets = append(packets, <-dest.packets)
	}
	state := readStage(t, s)
	if !state.Ending.Completed || len(state.Ending.Results) != 1 || !state.Ending.Results[0].Complete {
		t.Fatal("ending completion", state.Ending)
	}
	path := filepath.Join(t.TempDir(), "ending.flv")
	writeTestFLV(t, path, packets)
	frames := 0
	for _, m := range packets {
		if isVideoFrame(m) {
			frames++
		}
	}
	filter := fmt.Sprintf("select=eq(n\\,%d),crop=2:2:960:540", frames-1)
	pixel, err := exec.Command("ffmpeg", "-v", "error", "-i", path, "-vf", filter, "-frames:v", "1", "-pix_fmt", "rgb24", "-f", "rawvideo", "pipe:1").Output()
	if err != nil || len(pixel) < 3 || pixel[0] > 60 || pixel[1] < 180 || pixel[2] > 60 {
		t.Fatalf("final green ending frame was discarded: %v %v", pixel, err)
	}
	if dest.count() != 1 {
		t.Fatal("ordinary source change reconnected destination", dest.count())
	}
}

func TestContinuousRecoveryIsIsolatedAndStopCancelsRestart(t *testing.T) {
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
	processed, native := newSink(t), newSink(t)
	target := processed.target("processed")
	target.OutputMode = "continuous"
	s, address := startRelayWithLogger(t, []config.Target{target, native.target("native")}, slog.New(slog.NewTextHandler(io.Discard, nil)), true)
	if w := stageRequest(t, s, "go_live", nil); w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	ctx, cancel := context.WithCancel(context.Background())
	publisher := exec.CommandContext(ctx, realFFmpeg, "-v", "error", "-nostdin", "-re", "-f", "lavfi", "-i", "testsrc2=size=320x180:rate=25", "-f", "lavfi", "-i", "sine=frequency=880:sample_rate=48000", "-c:v", "libx264", "-threads", "2", "-pix_fmt", "yuv420p", "-g", "25", "-bf", "0", "-c:a", "aac", "-ar", "48000", "-ac", "2", "-f", "flv", address+"/input-key-1234567890")
	if err := publisher.Start(); err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); _ = publisher.Wait() })
	readyDeadline := time.Now().Add(10 * time.Second)
	for {
		state := readStage(t, s)
		if state.Continuous.Source == "obs" && processed.count() == 1 && native.count() == 1 {
			break
		}
		if time.Now().After(readyDeadline) {
			t.Fatalf("initial readiness: %+v processed=%d native=%d", state, processed.count(), native.count())
		}
		time.Sleep(20 * time.Millisecond)
	}
	before := readTargetDashboard(t, s).Outputs[0].SkippedFrames
	time.Sleep(200 * time.Millisecond)
	if after := readTargetDashboard(t, s).Outputs[0].SkippedFrames; after != before {
		t.Fatalf("healthy continuous output counted native packets as skipped: %d -> %d", before, after)
	}
	killEncoder := func() {
		t.Helper()
		raw, err := os.ReadFile(pidFile)
		if err != nil {
			t.Fatal(err)
		}
		pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
		if err != nil {
			t.Fatal(err)
		}
		process, err := os.FindProcess(pid)
		if err != nil {
			t.Fatal(err)
		}
		if err := process.Kill(); err != nil {
			t.Fatal(err)
		}
	}
	killEncoder()
	eventually(t, func() bool { return readStage(t, s).Continuous.Restarts > 0 })
	eventually(t, func() bool { return processed.count() == 2 && readStage(t, s).Continuous.Source == "obs" })
	if native.count() != 1 {
		t.Fatal("encoder recovery interrupted passthrough", native.count())
	}
	// Even Stop followed immediately by a fresh start must discard the old live
	// buffer and encoder session, rather than hiding OFF between engine ticks.
	oldEpoch := readStage(t, s).Continuous.Epoch
	if w := stageRequest(t, s, "stop_now", nil); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := stageRequest(t, s, "go_live", nil); w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	eventually(t, func() bool { return readStage(t, s).Continuous.Epoch > oldEpoch })
	if got := readStage(t, s).Continuous; got.Source == "obs" {
		t.Fatal("fresh broadcast reused buffered media from before Stop", got)
	}
	restarted := time.Now().Add(10 * time.Second)
	for readStage(t, s).Continuous.Source != "obs" {
		if time.Now().After(restarted) {
			t.Fatal("fresh broadcast did not become ready", readStage(t, s).Continuous)
		}
		time.Sleep(20 * time.Millisecond)
	}
	for len(native.packets) > 0 {
		<-native.packets
	}
	if m := receive(t, native); m == nil {
		t.Fatal("passthrough stopped forwarding")
	}
	killEncoder()
	eventually(t, func() bool { return readStage(t, s).Continuous.State == "recovering" })
	if w := stageRequest(t, s, "stop_now", nil); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	eventually(t, func() bool { return readStage(t, s).Continuous.State == "off" })
	connections := processed.count()
	time.Sleep(2500 * time.Millisecond)
	if processed.count() != connections || readStage(t, s).Stage != "OFF" {
		t.Fatal("encoder recovery restarted a stopped broadcast")
	}
}

func TestContinuousOutputPreservesAudioVideoMarkerSync(t *testing.T) {
	requireFFmpeg(t)
	dest := newSink(t)
	target := dest.target("continuous")
	target.OutputMode = "continuous"
	s, address := startRelayWithLogger(t, []config.Target{target}, slog.New(slog.NewTextHandler(io.Discard, nil)), true)
	if w := stageRequest(t, s, "go_live", nil); w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	publisher := exec.CommandContext(ctx, "ffmpeg", "-v", "error", "-nostdin", "-re", "-f", "lavfi", "-i", "color=red:size=320x180:rate=25", "-f", "lavfi", "-i", "aevalsrc=if(lt(mod(t\\,1)\\,0.04)\\,0.7*sin(2*PI*2000*t)\\,0)|0.1*sin(2*PI*880*t):s=48000", "-t", "8", "-vf", "drawbox=x=0:y=0:w=32:h=32:color=white:t=fill:enable='eq(mod(n,25),0)'", "-c:v", "libx264", "-threads", "2", "-g", "25", "-bf", "2", "-c:a", "aac", "-ar", "48000", "-ac", "2", "-f", "flv", address+"/input-key-1234567890")
	var diagnostics bytes.Buffer
	publisher.Stderr = &diagnostics
	if err := publisher.Start(); err != nil {
		t.Fatal(err)
	}
	finished := make(chan error, 1)
	go func() { finished <- publisher.Wait() }()
	var packets []*rtmp.Message
	deadline := time.NewTimer(13 * time.Second)
	defer deadline.Stop()
capture:
	for {
		select {
		case m := <-dest.packets:
			packets = append(packets, m)
		case <-deadline.C:
			break capture
		}
	}
	cancel()
	if err := <-finished; err != nil {
		t.Fatalf("publisher %v %s", err, diagnostics.String())
	}
	if w := stageRequest(t, s, "stop_now", nil); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	path := filepath.Join(t.TempDir(), "sync.flv")
	writeTestFLV(t, path, packets)
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

func TestContinuousReadySceneSwitchesAndMonitorKeepOneHealthySession(t *testing.T) {
	requireFFmpeg(t)
	healthy, failing := newSink(t), newSink(t)
	first, second := healthy.target("healthy"), failing.target("recovering")
	first.OutputMode, second.OutputMode = "continuous", "continuous"
	s, address, _ := startDrainRelay(t, []config.Target{first, second})
	if w := stageRequest(t, s, "prestream", nil); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	publisher := exec.CommandContext(ctx, "ffmpeg", "-v", "error", "-nostdin", "-re", "-f", "lavfi", "-i", "color=red:size=320x180:rate=25", "-f", "lavfi", "-i", "sine=frequency=880:sample_rate=48000", "-c:v", "libx264", "-threads", "2", "-g", "25", "-bf", "0", "-c:a", "aac", "-ac", "2", "-f", "flv", address+"/input-key-1234567890")
	if err := publisher.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { cancel(); _ = publisher.Wait() }()
	// Buffer OBS before selecting it; the prepared prestream keeps playing.
	until := time.NewTimer(6 * time.Second)
	defer until.Stop()
	var packets []*rtmp.Message
buffer:
	for {
		select {
		case m := <-healthy.packets:
			packets = append(packets, m)
		case <-until.C:
			break buffer
		}
	}
	waitSource := func(source string, limit time.Duration) {
		t.Helper()
		started := time.Now()
		for time.Since(started) < limit {
			if readStage(t, s).Continuous.Source == source {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("source %s did not reach output within %s: %+v", source, limit, readStage(t, s).Continuous)
	}
	if w := stageRequest(t, s, "go_live", nil); w.Code != 200 && w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	eventually(t, func() bool { return readStage(t, s).Stage == "LIVE" })
	waitSource("obs", time.Second)
	if w := stageRequest(t, s, "brb", nil); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	waitSource("brb", time.Second)
	// The monitor packages the processed output itself, with authenticated access.
	httpServer := httptest.NewServer(s.Handler())
	defer httpServer.Close()
	monitorCtx, stopMonitor := context.WithTimeout(ctx, 5*time.Second)
	defer stopMonitor()
	request, _ := http.NewRequestWithContext(monitorCtx, "GET", httpServer.URL+"/api/broadcast-preview?output_mode=continuous", nil)
	request.SetBasicAuth(s.cfg.DashboardUsername, s.cfg.DashboardPassword)
	response, err := httpServer.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	data := make([]byte, 512)
	_, err = io.ReadFull(response.Body, data)
	response.Body.Close()
	if err != nil || response.StatusCode != 200 || response.Header.Get("X-Preview-Codecs") == "" {
		t.Fatalf("processed monitor: %d %v", response.StatusCode, err)
	}
	failing.disconnect()
	eventually(t, func() bool { return failing.count() >= 2 })
	if readStage(t, s).Stage != "BRB" {
		t.Fatal("OBS ended deliberate break")
	}
	if w := stageRequest(t, s, "go_live", nil); w.Code != 200 && w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	eventually(t, func() bool { return readStage(t, s).Stage == "LIVE" })
	waitSource("obs", time.Second)
	if healthy.count() != 1 {
		t.Fatalf("healthy output reconnected %d times", healthy.count())
	}
	if w := stageRequest(t, s, "stop_now", nil); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	for len(healthy.packets) > 0 {
		packets = append(packets, <-healthy.packets)
	}
	path := filepath.Join(t.TempDir(), "switches.flv")
	writeTestFLV(t, path, packets)
	if out, err := exec.Command("ffmpeg", "-v", "error", "-xerror", "-i", path, "-f", "null", "-").CombinedOutput(); err != nil {
		t.Fatalf("switch decode: %v %s", err, out)
	}
	var previous time.Duration
	count := 0
	for _, m := range packets {
		if isVideoFrame(m) {
			if count > 0 && m.Timestamp-previous != 40*time.Millisecond {
				t.Fatal("switch changed cadence", previous, m.Timestamp)
			}
			previous = m.Timestamp
			count++
		}
	}
	if count < 200 {
		t.Fatal("insufficient continuous frames", count)
	}
}

func TestContinuousAudioLossFallsBackAndRecoversWithoutReconnect(t *testing.T) {
	requireFFmpeg(t)
	destination := newSink(t)
	target := destination.target("continuous")
	target.OutputMode = "continuous"
	s, address, _ := startDrainRelay(t, []config.Target{target})
	if w := stageRequest(t, s, "go_live", nil); w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	startPublisher := func(dropAudio bool) (context.CancelFunc, <-chan error) {
		ctx, cancel := context.WithCancel(context.Background())
		args := []string{"-v", "error", "-nostdin", "-re", "-f", "lavfi", "-i", "color=red:size=320x180:rate=25", "-f", "lavfi", "-i", "sine=frequency=880:sample_rate=48000"}
		if dropAudio {
			args = append(args, "-af", "aselect='lt(t,6)'")
		}
		args = append(args, "-c:v", "libx264", "-threads", "2", "-g", "25", "-bf", "0", "-c:a", "aac", "-ac", "2", "-max_interleave_delta", "100000", "-f", "flv", address+"/input-key-1234567890")
		cmd := exec.CommandContext(ctx, "ffmpeg", args...)
		if err := cmd.Start(); err != nil {
			cancel()
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { done <- cmd.Wait(); close(done) }()
		return cancel, done
	}
	cancel, done := startPublisher(true)
	defer func() { cancel(); <-done }()
	wait := func(source string, seconds int) {
		t.Helper()
		deadline := time.Now().Add(time.Duration(seconds) * time.Second)
		for time.Now().Before(deadline) {
			if readStage(t, s).Continuous.Source == source {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatalf("expected %s: %+v", source, readStage(t, s).Continuous)
	}
	wait("obs", 8)
	wait("brb", 12)
	cancel()
	<-done
	cancel, done = startPublisher(false)
	wait("obs", 8)
	if stage := readStage(t, s); stage.Stage != "LIVE" || stage.Continuous.Restarts != 0 || destination.count() != 1 {
		t.Fatal("audio outage changed intent or output session", stage, destination.count())
	}
	if w := stageRequest(t, s, "stop_now", nil); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var packets []*rtmp.Message
	for len(destination.packets) > 0 {
		packets = append(packets, <-destination.packets)
	}
	path := filepath.Join(t.TempDir(), "audio-outage.flv")
	writeTestFLV(t, path, packets)
	if out, err := exec.Command("ffmpeg", "-v", "error", "-xerror", "-i", path, "-f", "null", "-").CombinedOutput(); err != nil {
		t.Fatalf("outage decode: %v %s", err, out)
	}
}
