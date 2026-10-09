package relay

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"math"
	"strings"

	amp4 "github.com/abema/go-mp4"
	"github.com/bluenviron/mediacommon/v2/pkg/codecs/h264"
	"github.com/bluenviron/mediacommon/v2/pkg/codecs/mpeg4audio"
	"github.com/kreuzhofer/restreamer/internal/config"
	"github.com/kreuzhofer/restreamer/internal/rtmp"
)

const brbFPSTolerance = 0.1

// Readiness checks encoded framing and initialization without decoding or
// re-encoding live video. A FLV keyframe flag alone can describe an empty or
// malformed access unit and must not activate a destination session.
func validLiveKeyframe(header, frame *rtmp.Message) bool {
	if header == nil || len(header.Body) < 12 || header.Body[0]&0x8f != 7 || header.Body[1] != 0 ||
		frame == nil || len(frame.Body) < 7 || frame.Body[0] != 0x17 || frame.Body[1] != 1 {
		return false
	}
	var avc amp4.AVCDecoderConfiguration
	avc.SetType(amp4.BoxTypeAvcC())
	if _, err := amp4.Unmarshal(bytes.NewReader(header.Body[5:]), uint64(len(header.Body)-5), &avc, amp4.Context{}); err != nil ||
		avc.ConfigurationVersion != 1 || len(avc.SequenceParameterSets) == 0 || len(avc.PictureParameterSets) == 0 || avc.LengthSizeMinusOne == 2 {
		return false
	}
	for _, parameter := range avc.SequenceParameterSets {
		var sps h264.SPS
		if len(parameter.NALUnit) < 4 || parameter.NALUnit[0]&0x1f != 7 || sps.Unmarshal(parameter.NALUnit) != nil {
			return false
		}
	}
	for _, parameter := range avc.PictureParameterSets {
		if len(parameter.NALUnit) < 2 || parameter.NALUnit[0]&0x9f != 8 {
			return false
		}
	}
	lengthBytes := int(avc.LengthSizeMinusOne) + 1
	payload := frame.Body[5:]
	idr := false
	for len(payload) > 0 {
		if len(payload) < lengthBytes {
			return false
		}
		size := 0
		for _, value := range payload[:lengthBytes] {
			size = size<<8 | int(value)
		}
		payload = payload[lengthBytes:]
		if size < 1 || size > len(payload) || payload[0]&0x80 != 0 {
			return false
		}
		if payload[0]&0x1f == 5 {
			if size < 2 {
				return false
			}
			idr = true
		}
		payload = payload[size:]
	}
	return idr
}

type brbMismatch struct {
	Field  string `json:"field"`
	Ingest any    `json:"ingest"`
	BRB    any    `json:"brb"`
}

// Only locally named fields and decoded numeric/boolean codec properties enter
// this report. Never include raw headers, metadata, URLs, keys or parser errors.
type brbInputCheck struct {
	media, source string
	ingest, brb   map[string]any
	mismatches    []brbMismatch
}

func (c *brbInputCheck) compare(field string, ingest, brb any) {
	c.ingest[field], c.brb[field] = ingest, brb
	if ingest != brb {
		c.mismatches = append(c.mismatches, brbMismatch{field, ingest, brb})
	}
}
func (c *brbInputCheck) Error() string {
	differences := make([]string, 0, len(c.mismatches))
	for _, m := range c.mismatches {
		differences = append(differences, fmt.Sprintf("%s: ingest=%v, BRB=%v", m.Field, m.Ingest, m.BRB))
	}
	return fmt.Sprintf("BRB %s mismatch (%s): %s", c.media, c.source, strings.Join(differences, "; "))
}

func (s *Server) validateBRBInput(m *rtmp.Message) error {
	if (m.Type != rtmp.Video && m.Type != rtmp.Audio) || len(m.Body) < 2 || m.Body[1] != 0 {
		return nil
	}
	s.broadcast.mu.Lock()
	if s.broadcast.media == nil {
		s.broadcast.mu.Unlock()
		return nil
	}
	cfg := s.broadcast.media.settings.Profile
	s.broadcast.mu.Unlock()
	check := &brbInputCheck{ingest: make(map[string]any), brb: make(map[string]any), mismatches: []brbMismatch{}}
	if m.Type == rtmp.Video {
		check.video(m.Body, cfg)
	} else {
		check.audio(m.Body, cfg)
	}
	compatible := len(check.mismatches) == 0
	attrs := []slog.Attr{
		slog.String("media", check.media), slog.String("source", check.source),
		slog.Bool("compatible", compatible), slog.Any("ingest", check.ingest),
		slog.Any("brb", check.brb), slog.Any("brb_profile", cfg),
		slog.Any("mismatches", check.mismatches),
	}
	if m.Type == rtmp.Video {
		attrs = append(attrs, slog.Float64("fps_tolerance", brbFPSTolerance))
	}
	if !compatible {
		attrs = append(attrs, slog.String("reason", check.Error()))
		s.log.LogAttrs(context.Background(), slog.LevelWarn, "input incompatible with BRB", attrs...)
		return check
	}
	// Record accepted headers too: a later audio rejection must not hide the
	// video settings that already matched, or vice versa. Media packets are silent.
	s.log.LogAttrs(context.Background(), slog.LevelInfo, "input BRB compatibility checked", attrs...)
	return nil
}

func (c *brbInputCheck) video(body []byte, cfg config.BRBProfile) {
	c.media, c.source = "video", "h264_sps_vui"
	c.brb = map[string]any{"codec_id": 7, "width": cfg.Width, "height": cfg.Height, "fps": cfg.FPS, "bit_depth_luma": 8, "bit_depth_chroma": 8, "chroma_format_idc": 1, "nal_length_bytes": 4, "sps_count": 1, "pps_count": 1}
	c.compare("codec_id", int(body[0]&0xf), 7)
	c.compare("enhanced_rtmp", body[0]&0x80 != 0, false)
	if len(body) < 12 {
		c.compare("avc_configuration", "missing_or_truncated", "valid")
		return
	}
	var avc amp4.AVCDecoderConfiguration
	avc.SetType(amp4.BoxTypeAvcC())
	if _, err := amp4.Unmarshal(bytes.NewReader(body[5:]), uint64(len(body)-5), &avc, amp4.Context{}); err != nil {
		c.compare("avc_configuration", "invalid", "valid")
		return
	}
	c.compare("nal_length_bytes", int(avc.LengthSizeMinusOne)+1, 4)
	c.compare("sps_count", len(avc.SequenceParameterSets), 1)
	c.compare("pps_count", len(avc.PictureParameterSets), 1)
	if len(avc.SequenceParameterSets) != 1 {
		return
	}
	var sps h264.SPS
	if sps.Unmarshal(avc.SequenceParameterSets[0].NALUnit) != nil {
		c.compare("sps", "invalid", "valid")
		return
	}
	c.compare("width", sps.Width(), cfg.Width)
	c.compare("height", sps.Height(), cfg.Height)
	c.compare("bit_depth_luma", int(sps.BitDepthLumaMinus8)+8, 8)
	c.compare("bit_depth_chroma", int(sps.BitDepthChromaMinus8)+8, 8)
	c.compare("chroma_format_idc", int(sps.ChromaFormatIdc), 1)
	c.ingest["h264_profile_idc"] = int(sps.ProfileIdc)
	c.ingest["h264_level_idc"] = int(sps.LevelIdc)
	c.ingest["frame_mbs_only_flag"] = sps.FrameMbsOnlyFlag
	c.timing(&sps, cfg.FPS)
}

func (c *brbInputCheck) timing(sps *h264.SPS, expected int) {
	c.brb["fps"] = expected
	c.ingest["fps_formula"] = "time_scale / (2 * num_units_in_tick)"
	if sps.VUI == nil || sps.VUI.TimingInfo == nil {
		// Preserve support for publishers without VUI timing. Absence is not 0 fps
		// and does not make the arrival-rate graph an alternative source of truth.
		c.ingest["fps"] = "not_signalled"
		c.ingest["fps_check"] = "not_performed_no_vui_timing"
		return
	}
	timing := sps.VUI.TimingInfo
	c.ingest["time_scale"] = timing.TimeScale
	c.ingest["num_units_in_tick"] = timing.NumUnitsInTick
	c.ingest["fixed_frame_rate_flag"] = timing.FixedFrameRateFlag
	if timing.TimeScale == 0 || timing.NumUnitsInTick == 0 {
		c.ingest["fps"] = "unavailable_invalid_timing"
		c.compare("vui_timing", "zero_time_scale_or_num_units_in_tick", "nonzero_time_scale_and_num_units_in_tick")
		return
	}
	fps := sps.FPS()
	c.ingest["fps"] = fps
	c.ingest["fps_check"] = "compared_to_active_brb_profile"
	if math.Abs(fps-float64(expected)) > brbFPSTolerance {
		c.mismatches = append(c.mismatches, brbMismatch{"fps", fps, expected})
	}
}

func (c *brbInputCheck) audio(body []byte, cfg config.BRBProfile) {
	c.media, c.source = "audio", "aac_audio_specific_config"
	c.brb = map[string]any{"codec_id": 10, "audio_object_type": 2, "extension_type": 0, "sample_rate": cfg.SampleRate, "channels": 2, "frame_length_flag": false, "depends_on_core_coder": false}
	c.compare("codec_id", int(body[0]>>4), 10)
	var audio mpeg4audio.AudioSpecificConfig
	if audio.Unmarshal(body[2:]) != nil {
		c.compare("audio_specific_config", "invalid_or_unsupported", "valid AAC-LC")
		return
	}
	c.compare("audio_object_type", int(audio.Type), 2)
	c.compare("extension_type", int(audio.ExtensionType), 0)
	c.compare("sample_rate", audio.SampleRate, cfg.SampleRate)
	// ChannelConfig 0 is explicitly unknown until a PCE is parsed; do not claim
	// the input has zero channels. This path retains the existing rejection.
	if audio.ChannelConfig == 0 {
		c.compare("channels", "not_signalled_in_audio_specific_config", 2)
	} else {
		c.compare("channels", audio.ChannelCount, 2)
	}
	c.ingest["channel_configuration"] = int(audio.ChannelConfig)
	c.ingest["extension_sample_rate"] = audio.ExtensionSampleRate
	c.compare("frame_length_flag", audio.FrameLengthFlag, false)
	c.compare("depends_on_core_coder", audio.DependsOnCoreCoder, false)
}
