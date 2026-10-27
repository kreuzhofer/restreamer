package relay

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"time"

	"github.com/kreuzhofer/restreamer/internal/config"
	"github.com/kreuzhofer/restreamer/internal/rtmp"
)

// The video stays encoded. One audio-only encoder mixes two disk-backed loops,
// so neither a visual boundary nor a playlist boundary resets the other clock.
// Its pipe and bounded packet channel apply backpressure during pauses.
type presentationSource struct {
	visualPath, scenePath, musicPath string
	index                            *clipIndex
	profile                          config.BRBProfile
	gain                             float64
	runtime                          *presentationPlayback
}
type presentationPlayback struct {
	video                      *clipReader
	cancel                     context.CancelFunc
	audio                      <-chan *rtmp.Message
	done                       <-chan struct{}
	videoFrame                 int64
	pendingVideo, pendingAudio *rtmp.Message
	waitingSince               time.Time
}

func (p *presentationSource) start() error {
	if p.runtime != nil {
		return nil
	}
	video, err := openClip(p.visualPath, p.index)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	args := append(generatorBaseArgs(), presentationAudioInputs(p.scenePath, p.musicPath, true)...)
	args = append(args, "-filter_complex", fmt.Sprintf("[0:a][1:a]amix=inputs=2:duration=longest:normalize=0,volume=%.12f[a]", p.gain), "-map", "[a]", "-c:a", "aac", "-ar", strconv.Itoa(p.profile.SampleRate), "-ac", "2", "-b:a", "128k", "-threads", "1", "-flush_packets", "1", "-f", "flv", "pipe:1")
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	pipe, err := cmd.StdoutPipe()
	if err == nil {
		err = cmd.Start()
	}
	if err != nil {
		cancel()
		video.close()
		return errors.New("Cannot start presentation audio encoder")
	}
	packets := make(chan *rtmp.Message, 32)
	done := make(chan struct{})
	p.runtime = &presentationPlayback{video: video, cancel: cancel, audio: packets, done: done, waitingSince: time.Now()}
	go func() {
		defer close(done)
		defer close(packets)
		defer cancel()
		_ = readPresentationAudio(ctx, pipe, packets)
		cancel()
		_ = cmd.Wait()
	}()
	return nil
}
func (p *presentationSource) close() {
	if p.runtime == nil {
		return
	}
	p.runtime.cancel()
	p.runtime.video.close()
	p.runtime = nil
}

// Validate every FLV tag before allocating a body. EOF or encoder failure is
// surfaced by the closed channel; stderr is never exposed to operators.
func readPresentationAudio(ctx context.Context, r io.Reader, out chan<- *rtmp.Message) error {
	var header [13]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return err
	}
	if string(header[:3]) != "FLV" || binary.BigEndian.Uint32(header[5:9]) != 9 {
		return errors.New("invalid audio container")
	}
	var timestampBase int64
	for {
		var h [11]byte
		if _, err := io.ReadFull(r, h[:]); err != nil {
			return err
		}
		n := int(h[1])<<16 | int(h[2])<<8 | int(h[3])
		if n < 2 || n > maxClipPacket {
			return errors.New("invalid audio packet size")
		}
		body := make([]byte, n)
		if _, err := io.ReadFull(r, body); err != nil {
			return err
		}
		var tail [4]byte
		if _, err := io.ReadFull(r, tail[:]); err != nil {
			return err
		}
		if binary.BigEndian.Uint32(tail[:]) != uint32(n+11) {
			return errors.New("invalid audio packet")
		}
		if h[0] != rtmp.Audio || body[1] != 1 {
			continue
		}
		// Preserve the encoder's continuous millisecond timestamps. The first AAC
		// packet includes its one-time priming; subsequent cycles never re-prime.
		ts := uint32(h[7])<<24 | uint32(h[4])<<16 | uint32(h[5])<<8 | uint32(h[6])
		if timestampBase == 0 {
			timestampBase = int64(ts) + 1
		}
		msg := &rtmp.Message{Type: rtmp.Audio, Timestamp: time.Duration(int64(ts)-(timestampBase-1)) * time.Millisecond, Body: body}
		select {
		case out <- msg:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (b *broadcast) startPresentation(now time.Time) error {
	p := b.clip
	if err := p.presentation.start(); err != nil {
		return err
	}
	p.started = now
	p.startPosition = p.position
	p.broadcastBase = max(b.last+time.Millisecond, now.Sub(b.started))
	p.streaming = true
	p.eof = false
	b.live, b.active = false, false
	b.hub.clearHeaders()
	b.resetBroadcastPreview()
	b.emit(p.presentation.index.Video, p.broadcastBase)
	b.emit(p.reader.index.Audio, p.broadcastBase)
	p.presentation.runtime.waitingSince = time.Now()
	return nil
}
func (b *broadcast) tickPresentation(now time.Time) bool {
	p := b.clip
	if !p.streaming {
		if err := b.startPresentation(now); err != nil {
			b.failPlayback("Cannot start presentation audio; check FFmpeg and Retry", now)
			return false
		}
	}
	r := p.presentation.runtime
	elapsed := p.position + max(0, now.Sub(p.started))
	for sent := 0; sent < 256; sent++ {
		if r.pendingVideo == nil {
			for reads := 0; reads < 256; reads++ {
				msg, err := r.video.next()
				if errors.Is(err, io.EOF) {
					if _, err = r.video.seek(0); err != nil {
						b.failPlayback("Cannot repeat presentation video; prepare it again and Retry", now)
						return false
					}
					continue
				}
				if err != nil {
					b.failPlayback("Cannot read presentation video; prepare it again and Retry", now)
					return false
				}
				if msg.Type != rtmp.Video {
					continue
				}
				msg.Timestamp = time.Duration(r.videoFrame) * time.Second / time.Duration(p.presentation.profile.FPS)
				r.videoFrame++
				r.pendingVideo = msg
				break
			}
			if r.pendingVideo == nil {
				b.failPlayback("Presentation video has no usable frames; prepare it again and Retry", now)
				return false
			}
		}
		if r.pendingAudio == nil {
			select {
			case msg, ok := <-r.audio:
				if !ok {
					b.failPlayback("Presentation audio stopped; check FFmpeg and storage, then Retry", now)
					return false
				}
				r.pendingAudio = msg
				r.waitingSince = time.Now()
			default:
				if time.Since(r.waitingSince) > 5*time.Second {
					b.failPlayback("Presentation audio stalled; check server load and Retry", now)
					return false
				}
			}
		}
		var msg *rtmp.Message
		if r.pendingVideo.Timestamp <= elapsed {
			msg = r.pendingVideo
		}
		if r.pendingAudio != nil && r.pendingAudio.Timestamp <= elapsed && (msg == nil || r.pendingAudio.Timestamp < msg.Timestamp) {
			msg = r.pendingAudio
		}
		if msg == nil {
			return true
		}
		b.emit(msg, p.broadcastBase+msg.Timestamp-p.startPosition)
		if msg == r.pendingVideo {
			r.pendingVideo = nil
		} else {
			r.pendingAudio = nil
		}
	}
	return true
}
