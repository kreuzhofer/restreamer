package relay

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	amp4 "github.com/abema/go-mp4"
	"github.com/bluenviron/mediacommon/v2/pkg/formats/fmp4"
	"github.com/bluenviron/mediacommon/v2/pkg/formats/fmp4/seekablebuffer"
	"github.com/bluenviron/mediacommon/v2/pkg/formats/mp4/codecs"
	"github.com/kreuzhofer/restreamer/internal/rtmp"
)

var errPreviewCodec = errors.New("Preview requires browser-compatible H.264 and AAC-LC configuration")

// Each viewer repackages encoded packets, without decoding or transcoding.
// Its bounded subscription can overflow independently of destination queues.
func (s *Server) preview(w http.ResponseWriter, r *http.Request) {
	select {
	case s.previewSlots <- struct{}{}:
		defer func() { <-s.previewSlots }()
	default:
		http.Error(w, "Preview viewer limit reached", http.StatusTooManyRequests)
		return
	}
	s.previewMu.Lock()
	h, done := s.previewHub, s.previewDone
	s.previewMu.Unlock()
	var epoch uint64
	if r.URL.Path == "/api/broadcast-preview" {
		if s.broadcast == nil {
			http.Error(w, "Broadcast is OFF", 503)
			return
		}
		s.broadcast.mu.Lock()
		if s.broadcast.control.stage == "OFF" {
			s.broadcast.mu.Unlock()
			http.Error(w, "Broadcast is OFF", 503)
			return
		}
		h = s.broadcast.hub
		done = s.broadcast.previewChanged
		epoch = s.broadcast.previewEpoch
		s.broadcast.mu.Unlock()
	}
	if h == nil {
		http.Error(w, "Waiting for OBS input", http.StatusServiceUnavailable)
		return
	}
	sub := h.subscribe()
	defer h.unsubscribe(sub)
	rc := http.NewResponseController(w)
	// Streaming responses refresh their write deadline per chunk. Other HTTP
	// routes retain the server's short timeout.
	_ = rc.SetWriteDeadline(time.Now().Add(15 * time.Second))
	timer := time.NewTimer(12 * time.Second)
	defer timer.Stop()
	mux := previewMux{}
	started := false
	for {
		select {
		case <-r.Context().Done():
			return
		case <-done:
			return
		case <-sub.failed:
			return
		case <-timer.C:
			if !started {
				http.Error(w, "Preview timed out waiting for video and a keyframe", http.StatusGatewayTimeout)
			}
			return
		case msg := <-sub.packets:
			h.consumed(sub, msg)
			data, err := mux.push(msg)
			if err != nil {
				if !started {
					http.Error(w, err.Error(), http.StatusUnprocessableEntity)
				}
				return
			}
			if len(data) == 0 {
				continue
			}
			if !started {
				w.Header().Set("Content-Type", "video/mp4")
				w.Header().Set("X-Preview-Codecs", mux.mime)
				w.Header().Set("X-Preview-Base-Ms", strconv.FormatInt(mux.base.Milliseconds(), 10))
				w.Header().Set("X-Preview-Epoch", strconv.FormatUint(epoch, 10))
				w.Header().Set("X-Accel-Buffering", "no")
				started = true
			}
			_ = rc.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if _, err = w.Write(data); err != nil {
				return
			}
			if err = rc.Flush(); err != nil {
				return
			}
			timer.Reset(12 * time.Second)
		}
	}
}

type previewMux struct {
	video, audio []byte
	videoCodec   *codecs.H264
	audioCodec   *codecs.MPEG4Audio
	mime         string
	started      bool
	base         time.Duration
	pending      *rtmp.Message
	sequence     uint32
}

func (p *previewMux) push(m *rtmp.Message) ([]byte, error) {
	if m.Type != rtmp.Video && m.Type != rtmp.Audio {
		return nil, nil
	}
	if len(m.Body) < 2 {
		return nil, errPreviewCodec
	}
	if m.Body[1] == 0 {
		if m.Type == rtmp.Video {
			if bytes.Equal(p.video, m.Body) {
				return nil, nil
			}
			if p.started || len(m.Body) < 12 {
				return nil, errPreviewCodec
			}
			var config amp4.AVCDecoderConfiguration
			config.SetType(amp4.BoxTypeAvcC())
			_, err := amp4.Unmarshal(bytes.NewReader(m.Body[5:]), uint64(len(m.Body)-5), &config, amp4.Context{})
			if err != nil || config.LengthSizeMinusOne != 3 || len(config.SequenceParameterSets) != 1 || len(config.PictureParameterSets) != 1 || len(config.SequenceParameterSets[0].NALUnit) < 4 {
				return nil, errPreviewCodec
			}
			p.videoCodec = &codecs.H264{SPS: config.SequenceParameterSets[0].NALUnit, PPS: config.PictureParameterSets[0].NALUnit}
			p.video = m.Body
		} else {
			if bytes.Equal(p.audio, m.Body) {
				return nil, nil
			}
			if p.started {
				return nil, errPreviewCodec
			}
			codec := &codecs.MPEG4Audio{}
			if err := codec.Config.Unmarshal(m.Body[2:]); err != nil || codec.Config.Type != 2 || codec.Config.ExtensionType != 0 || codec.Config.FrameLengthFlag || codec.Config.DependsOnCoreCoder {
				return nil, errPreviewCodec
			}
			p.audioCodec = codec
			p.audio = m.Body
		}
		return nil, nil
	}
	if !p.started {
		if !isVideoFrame(m) || m.Body[0]>>4 != 1 || p.videoCodec == nil {
			return nil, nil
		}
		tracks := []*fmp4.InitTrack{{ID: 1, TimeScale: 1000, Codec: p.videoCodec}}
		codecString := fmt.Sprintf("avc1.%02x%02x%02x", p.videoCodec.SPS[1], p.videoCodec.SPS[2], p.videoCodec.SPS[3])
		if p.audioCodec != nil {
			tracks = append(tracks, &fmp4.InitTrack{ID: 2, TimeScale: uint32(p.audioCodec.Config.SampleRate), Codec: p.audioCodec})
			codecString += ",mp4a.40.2"
		}
		var buf seekablebuffer.Buffer
		if err := (fmp4.Init{Tracks: tracks}).Marshal(&buf); err != nil {
			return nil, errPreviewCodec
		}
		p.mime = `video/mp4; codecs="` + codecString + `"`
		p.base, p.pending, p.started = m.Timestamp, m, true
		return buf.Bytes(), nil
	}
	if m.Timestamp < p.base {
		return nil, errors.New("Preview timestamps moved backwards")
	}
	var track *fmp4.PartTrack
	if isVideoFrame(m) {
		prev := p.pending
		delta := (m.Timestamp - prev.Timestamp) / time.Millisecond
		if delta <= 0 || delta > 10000 {
			return nil, errors.New("Preview video timing discontinuity")
		}
		// AVC composition time is a signed 24-bit millisecond offset (B frames).
		offset := int32(prev.Body[2])<<16 | int32(prev.Body[3])<<8 | int32(prev.Body[4])
		offset = offset << 8 >> 8
		track = &fmp4.PartTrack{ID: 1, BaseTime: uint64((prev.Timestamp - p.base) / time.Millisecond), Samples: []*fmp4.Sample{{Duration: uint32(delta), PTSOffset: offset, IsNonSyncSample: prev.Body[0]>>4 != 1, Payload: prev.Body[5:]}}}
		p.pending = m
	} else if m.Type == rtmp.Audio && m.Body[1] == 1 && p.audioCodec != nil {
		track = &fmp4.PartTrack{ID: 2, BaseTime: uint64((m.Timestamp-p.base)/time.Millisecond) * uint64(p.audioCodec.Config.SampleRate) / 1000, Samples: []*fmp4.Sample{{Duration: 1024, Payload: m.Body[2:]}}}
	} else {
		return nil, nil
	}
	p.sequence++
	var buf seekablebuffer.Buffer
	if err := (fmp4.Part{SequenceNumber: p.sequence, Tracks: []*fmp4.PartTrack{track}}).Marshal(&buf); err != nil {
		return nil, errors.New("Cannot package preview media")
	}
	return buf.Bytes(), nil
}
