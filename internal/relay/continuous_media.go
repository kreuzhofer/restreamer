package relay

// The continuous path uses FFmpeg only as a codec worker. Selection, bounded
// admission, the program clock and cancellation remain owned by the Go server.
import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kreuzhofer/restreamer/internal/rtmp"
)

const continuousFPS = 25
const continuousFrameBytes = 1920 * 1080 * 3 / 2
const continuousAudioBytes = 48000 / continuousFPS * 2 * 2
const continuousFrameTime = time.Second / continuousFPS

type programFrame struct {
	video, audio     []byte
	ingress, decoded time.Time
}

type ingressFrame struct {
	pts time.Duration
	at  time.Time
}

type programDecoder struct {
	frameLimit    int
	dropped       uint64
	mu            sync.Mutex
	frames        []*programFrame
	ingress       []ingressFrame
	queuedBytes   int
	base          time.Duration
	primed, ended bool
	packets       chan *rtmp.Message
	ctx           context.Context
	cancel        context.CancelFunc
	cmd           *exec.Cmd
	in            io.WriteCloser
	video         io.ReadCloser
	audio         *os.File
	done          chan struct{}
	wg            sync.WaitGroup
}

func startProgramDecoder(parent context.Context, base, presentationOrigin time.Duration, frameLimit int) (*programDecoder, error) {
	ctx, cancel := context.WithCancel(parent)
	audioRead, audioWrite, err := os.Pipe()
	if err != nil {
		cancel()
		return nil, err
	}
	d := &programDecoder{frameLimit: frameLimit, base: base, ctx: ctx, cancel: cancel, packets: make(chan *rtmp.Message, 512), audio: audioRead, done: make(chan struct{})}
	// A keyframe can be decoded before its presentation time (B-frame
	// reordering). Start both raw streams at that same presentation origin;
	// padding the first picture back to DTS zero would show it before its audio.
	origin := strconv.FormatFloat(presentationOrigin.Seconds(), 'f', 3, 64)
	videoFilter := "setpts=PTS-(" + origin + ")/TB,scale=1920:1080:force_original_aspect_ratio=decrease,pad=1920:1080:(ow-iw)/2:(oh-ih)/2,fps=25:start_time=0,format=yuv420p"
	audioFilter := "atrim=start=" + origin + ",asetpts=PTS-(" + origin + ")/TB,aresample=48000:async=1000:min_hard_comp=0.001:first_pts=0"
	d.cmd = exec.CommandContext(ctx, "ffmpeg", "-hide_banner", "-nostdin", "-v", "error", "-protocol_whitelist", "pipe,file", "-threads", "2", "-probesize", "32768", "-analyzeduration", "100000", "-f", "flv", "-i", "pipe:0", "-filter_threads", "1", "-map", "0:v:0", "-vf", videoFilter, "-c:v", "rawvideo", "-threads", "1", "-f", "rawvideo", "pipe:1", "-map", "0:a:0", "-af", audioFilter, "-ac", "2", "-f", "s16le", "pipe:3")
	d.cmd.ExtraFiles = []*os.File{audioWrite}
	// Decoder diagnostics may contain untrusted input; expose only safe states.
	d.cmd.Stderr = io.Discard
	d.in, err = d.cmd.StdinPipe()
	if err == nil {
		d.video, err = d.cmd.StdoutPipe()
	}
	if err == nil {
		err = d.cmd.Start()
	}
	audioWrite.Close()
	if err != nil {
		cancel()
		audioRead.Close()
		if d.in != nil {
			d.in.Close()
		}
		if d.video != nil {
			d.video.Close()
		}
		return nil, errors.New("Cannot start continuous source decoder; check FFmpeg")
	}
	d.wg.Add(3)
	go func() {
		defer d.wg.Done()
		defer d.in.Close()
		if _, err := d.in.Write(flvStreamHeader); err != nil {
			return
		}
		for {
			select {
			case <-ctx.Done():
				return
			case m, ok := <-d.packets:
				if !ok {
					return
				}
				d.mu.Lock()
				d.queuedBytes -= len(m.Body)
				d.mu.Unlock()
				if writeProgramTag(d.in, m) != nil {
					return
				}
			}
		}
	}()
	videos, audios := make(chan []byte, 4), make(chan []byte, 4)
	read := func(r io.Reader, size int, out chan []byte) {
		defer d.wg.Done()
		defer close(out)
		for {
			data := make([]byte, size)
			if _, err := io.ReadFull(r, data); err != nil {
				return
			}
			select {
			case out <- data:
			case <-ctx.Done():
				return
			}
		}
	}
	go read(d.video, continuousFrameBytes, videos)
	go read(d.audio, continuousAudioBytes, audios)
	go func() {
		defer close(d.done)
		sequence := presentationOrigin
		for {
			var video, audio []byte
			var ok bool
			select {
			case video, ok = <-videos:
			case <-ctx.Done():
			}
			if !ok {
				break
			}
			select {
			case audio, ok = <-audios:
			case <-ctx.Done():
			}
			if !ok {
				break
			}
			now := time.Now()
			d.mu.Lock()
			at := time.Time{}
			nearestPTS := time.Duration(-1 << 63)
			for _, candidate := range d.ingress {
				if candidate.pts <= sequence+continuousFrameTime/2 && (at.IsZero() || candidate.pts > nearestPTS) {
					at, nearestPTS = candidate.at, candidate.pts
				}
			}
			if len(d.frames) == d.frameLimit {
				d.dropped++
				copy(d.frames, d.frames[1:])
				d.frames = d.frames[:d.frameLimit-1]
			}
			d.frames = append(d.frames, &programFrame{video: video, audio: audio, ingress: at, decoded: now})
			d.mu.Unlock()
			sequence += continuousFrameTime
		}
		cancel()
		d.in.Close()
		d.video.Close()
		d.audio.Close()
		d.wg.Wait()
		_ = d.cmd.Wait()
	}()
	return d, nil
}

// Called only for an AVC video frame, after its five-byte header is validated.
func programCompositionOffset(m *rtmp.Message) time.Duration {
	cts := int32(m.Body[2])<<16 | int32(m.Body[3])<<8 | int32(m.Body[4])
	if cts&0x800000 != 0 {
		cts -= 1 << 24
	}
	return time.Duration(cts) * time.Millisecond
}

// Feed never blocks the publisher or broadcast controller. Overflow invalidates
// this decoder; the engine can rejoin at a fresh keyframe without unbounded lag.
func (d *programDecoder) feed(m *rtmp.Message, at time.Time) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.ended || d.ctx.Err() != nil {
		return false
	}
	copy := *m
	copy.Timestamp = max(0, m.Timestamp-d.base)
	if isVideoFrame(m) {
		pts := copy.Timestamp + programCompositionOffset(m)
		if len(d.ingress) == 512 {
			copyIngress := d.ingress[1:]
			d.ingress = append(d.ingress[:0], copyIngress...)
		}
		d.ingress = append(d.ingress, ingressFrame{pts, at})
	}
	if d.queuedBytes+len(m.Body) > 16<<20 {
		d.cancel()
		return false
	}
	select {
	case d.packets <- &copy:
		d.queuedBytes += len(m.Body)
		return true
	default:
		d.cancel()
		return false
	}
}

func (d *programDecoder) end() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.ended {
		d.ended = true
		close(d.packets)
	}
}
func (d *programDecoder) close() {
	d.cancel()
	d.in.Close()
	d.video.Close()
	d.audio.Close()
	<-d.done
}

func (d *programDecoder) drained() bool {
	select {
	case <-d.done:
	default:
		return false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.frames) == 0
}

func (d *programDecoder) queueStatus() (int, uint64) {
	if d == nil {
		return 0, 0
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.frames), d.dropped
}

func (d *programDecoder) next(now time.Time, delay time.Duration) *programFrame {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.frames) == 0 {
		d.primed = false
		return nil
	}
	if !d.primed {
		origin := d.frames[0].ingress
		if origin.IsZero() {
			origin = d.frames[0].decoded
		}
		if now.Sub(origin) < delay {
			return nil
		}
		d.primed = true
	}
	f := d.frames[0]
	d.frames[0] = nil
	d.frames = d.frames[1:]
	return f
}

var flvStreamHeader = []byte{'F', 'L', 'V', 1, 5, 0, 0, 0, 9, 0, 0, 0, 0}

func writeProgramTag(w io.Writer, m *rtmp.Message) error {
	n := len(m.Body)
	ts := uint32(m.Timestamp / time.Millisecond)
	header := [11]byte{m.Type, byte(n >> 16), byte(n >> 8), byte(n), byte(ts >> 16), byte(ts >> 8), byte(ts), byte(ts >> 24)}
	if _, err := w.Write(header[:]); err != nil {
		return err
	}
	if _, err := w.Write(m.Body); err != nil {
		return err
	}
	var tail [4]byte
	binary.BigEndian.PutUint32(tail[:], uint32(n+11))
	_, err := w.Write(tail[:])
	return err
}

func readProgramFLV(r io.Reader, emit func(*rtmp.Message) error) error {
	var header [13]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return err
	}
	if string(header[:3]) != "FLV" || binary.BigEndian.Uint32(header[5:9]) != 9 {
		return errors.New("invalid encoded stream header")
	}
	for {
		var h [11]byte
		if _, err := io.ReadFull(r, h[:]); err != nil {
			return err
		}
		n := int(h[1])<<16 | int(h[2])<<8 | int(h[3])
		if n > 16<<20 {
			return errors.New("encoded packet exceeds limit")
		}
		body := make([]byte, n+4)
		if _, err := io.ReadFull(r, body); err != nil {
			return err
		}
		if binary.BigEndian.Uint32(body[n:]) != uint32(n+11) {
			return errors.New("invalid encoded packet size")
		}
		ts := uint32(h[7])<<24 | uint32(h[4])<<16 | uint32(h[5])<<8 | uint32(h[6])
		if err := emit(&rtmp.Message{Type: h[0], Timestamp: time.Duration(ts) * time.Millisecond, Body: body[:n]}); err != nil {
			return err
		}
	}
}

type programEncoder struct {
	ready        atomic.Bool
	ctx          context.Context
	cancel       context.CancelFunc
	video, audio chan []byte
	done         chan struct{}
	mu           sync.Mutex
	err          error
}

func startProgramEncoder(parent context.Context, emit func(*rtmp.Message) error) (*programEncoder, error) {
	ctx, cancel := context.WithCancel(parent)
	ar, aw, err := os.Pipe()
	if err != nil {
		cancel()
		return nil, err
	}
	cmd := exec.CommandContext(ctx, "ffmpeg", "-hide_banner", "-nostdin", "-v", "error", "-thread_queue_size", "2", "-probesize", "32", "-analyzeduration", "0", "-f", "rawvideo", "-pixel_format", "yuv420p", "-video_size", "1920x1080", "-framerate", "25", "-i", "pipe:0", "-thread_queue_size", "2", "-probesize", "32", "-analyzeduration", "0", "-f", "s16le", "-ar", "48000", "-ac", "2", "-i", "pipe:3", "-map", "0:v:0", "-map", "1:a:0", "-c:v", "libx264", "-threads", "3", "-preset", "veryfast", "-tune", "zerolatency", "-pix_fmt", "yuv420p", "-b:v", "8000k", "-minrate", "8000k", "-maxrate", "8000k", "-bufsize", "16000k", "-g", "50", "-keyint_min", "50", "-sc_threshold", "0", "-x264-params", "nal-hrd=cbr:force-cfr=1", "-c:a", "aac", "-b:a", "160k", "-ar", "48000", "-ac", "2", "-f", "flv", "pipe:1")
	cmd.ExtraFiles = []*os.File{ar}
	cmd.Stderr = io.Discard
	video, err := cmd.StdinPipe()
	var output io.ReadCloser
	if err == nil {
		output, err = cmd.StdoutPipe()
	}
	if err == nil {
		err = cmd.Start()
	}
	ar.Close()
	if err != nil {
		cancel()
		aw.Close()
		if video != nil {
			video.Close()
		}
		if output != nil {
			output.Close()
		}
		return nil, errors.New("Cannot start continuous encoder; check FFmpeg and libx264")
	}
	e := &programEncoder{ctx: ctx, cancel: cancel, video: make(chan []byte, 2), audio: make(chan []byte, 2), done: make(chan struct{})}
	var writers sync.WaitGroup
	writers.Add(2)
	write := func(w io.WriteCloser, items <-chan []byte) {
		defer writers.Done()
		defer w.Close()
		for {
			select {
			case <-ctx.Done():
				return
			case b, ok := <-items:
				if !ok {
					return
				}
				if _, err := w.Write(b); err != nil {
					cancel()
					return
				}
			}
		}
	}
	go write(video, e.video)
	go write(aw, e.audio)
	go func() {
		defer close(e.done)
		err := readProgramFLV(output, func(m *rtmp.Message) error {
			if isVideoFrame(m) {
				e.ready.Store(true)
			}
			return emit(m)
		})
		if !errors.Is(err, io.EOF) {
			cancel()
		}
		video.Close()
		aw.Close()
		output.Close()
		writers.Wait()
		waitErr := cmd.Wait()
		cancel()
		e.mu.Lock()
		if err != nil && !errors.Is(err, io.EOF) {
			e.err = errors.New("Continuous encoder output ended unexpectedly")
		}
		if waitErr != nil {
			e.err = errors.New("Continuous encoder stopped unexpectedly")
		}
		e.mu.Unlock()
	}()
	return e, nil
}

func (e *programEncoder) put(frame *programFrame) bool {
	timeout := 200 * time.Millisecond
	if !e.ready.Load() {
		timeout = 2 * time.Second
	}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for i, data := range [][]byte{frame.video, frame.audio} {
		target := e.video
		if i == 1 {
			target = e.audio
		}
		select {
		case target <- data:
		case <-e.ctx.Done():
			return false
		case <-deadline.C:
			e.cancel()
			return false
		}
	}
	return true
}
func (e *programEncoder) close() { e.cancel(); <-e.done }

func (e *programEncoder) finish() { close(e.video); close(e.audio) }
