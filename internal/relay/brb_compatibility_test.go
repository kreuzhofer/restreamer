package relay

import (
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/bluenviron/mediacommon/v2/pkg/codecs/h264"
	"github.com/bluenviron/mediacommon/v2/pkg/codecs/mpeg4audio"
	"github.com/kreuzhofer/restreamer/internal/config"
	"github.com/kreuzhofer/restreamer/internal/rtmp"
)

func diagnosticBRB(t *testing.T, profile config.BRBProfile) (*Server, *logBuffer) {
	t.Helper()
	s := dashboardServer(t)
	logs := new(logBuffer)
	s.log = slog.New(slog.NewJSONHandler(logs, nil))
	// The saved dashboard profile must win over the initial configuration.
	s.cfg.BRB = &config.BRBConfig{BRBProfile: config.BRBProfile{Width: 3840, Height: 2160, FPS: 60, SampleRate: 48000}}
	s.broadcast = newBroadcast(s, &brbMedia{settings: brbSettings{Profile: profile}})
	return s, logs
}
func compatibilityLog(t *testing.T, logs *logBuffer) map[string]any {
	t.Helper()
	var entry map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(logs.String())), &entry); err != nil {
		t.Fatalf("expected one structured compatibility log: %v\n%s", err, logs.String())
	}
	return entry
}
func assertMismatch(t *testing.T, entry map[string]any, field string, ingest, brb any) {
	t.Helper()
	mismatches, ok := entry["mismatches"].([]any)
	if !ok {
		t.Fatal("missing mismatches", entry)
	}
	for _, item := range mismatches {
		m := item.(map[string]any)
		if m["field"] == field {
			if m["ingest"] != ingest || m["brb"] != brb {
				t.Fatalf("wrong %s mismatch: %+v", field, m)
			}
			return
		}
	}
	t.Fatalf("missing %s mismatch: %+v", field, mismatches)
}

func TestBRBVideoDiagnosticsReportAllDifferences(t *testing.T) {
	s, logs := diagnosticBRB(t, config.BRBProfile{Width: 1280, Height: 720, FPS: 25, SampleRate: 48000})
	err := s.validateBRBInput(previewVideoConfig())
	if err == nil {
		t.Fatal("mismatch accepted")
	}
	for _, want := range []string{"width: ingest=1920, BRB=1280", "height: ingest=1080, BRB=720", "fps: ingest=30, BRB=25"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("dashboard error missing %q: %s", want, err)
		}
	}
	entry := compatibilityLog(t, logs)
	if entry["msg"] != "input incompatible with BRB" || entry["source"] != "h264_sps_vui" {
		t.Fatal("unidentified diagnostic", entry)
	}
	assertMismatch(t, entry, "width", float64(1920), float64(1280))
	assertMismatch(t, entry, "height", float64(1080), float64(720))
	assertMismatch(t, entry, "fps", float64(30), float64(25))
	ingest := entry["ingest"].(map[string]any)
	if ingest["time_scale"] != float64(60) || ingest["num_units_in_tick"] != float64(1) || ingest["fps_formula"] != "time_scale / (2 * num_units_in_tick)" {
		t.Fatal("missing raw timing evidence", ingest)
	}
	if _, ok := ingest["fixed_frame_rate_flag"].(bool); !ok {
		t.Fatal("missing fixed frame rate flag")
	}
	if entry["fps_tolerance"] != 0.1 {
		t.Fatal("missing comparison tolerance")
	}
}

func TestBRBFPSCompatibilityDoesNotUseObservedPacketRate(t *testing.T) {
	s, logs := diagnosticBRB(t, config.BRBProfile{Width: 1920, Height: 1080, FPS: 30, SampleRate: 48000})
	start := time.Now()
	s.sample(start)
	s.inputFrames.Add(75)
	s.sample(start.Add(time.Second))
	if s.history(start.Add(time.Second))[0].InputFPS != 75 {
		t.Fatal("incorrect observed rate fixture")
	}
	if err := s.validateBRBInput(previewVideoConfig()); err != nil {
		t.Fatal(err)
	}
	entry := compatibilityLog(t, logs)
	if entry["compatible"] != true || entry["ingest"].(map[string]any)["fps"] != float64(30) || entry["brb"].(map[string]any)["fps"] != float64(30) {
		t.Fatal("compatibility used graph rate or startup profile", entry)
	}
}

func TestBRBAudioDiagnosticsReportValuesAndFlags(t *testing.T) {
	s, logs := diagnosticBRB(t, config.BRBProfile{Width: 1920, Height: 1080, FPS: 25, SampleRate: 48000})
	audio := mpeg4audio.AudioSpecificConfig{Type: mpeg4audio.ObjectTypeAACLC, SampleRate: 44100, ChannelCount: 1, FrameLengthFlag: true, DependsOnCoreCoder: true}
	body, err := audio.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	err = s.validateBRBInput(packet(rtmp.Audio, 0, append([]byte{0xaf, 0}, body...)...))
	if err == nil {
		t.Fatal("invalid audio accepted")
	}
	entry := compatibilityLog(t, logs)
	if entry["source"] != "aac_audio_specific_config" {
		t.Fatal("missing audio source")
	}
	assertMismatch(t, entry, "sample_rate", float64(44100), float64(48000))
	assertMismatch(t, entry, "channels", float64(1), float64(2))
	assertMismatch(t, entry, "frame_length_flag", true, false)
	assertMismatch(t, entry, "depends_on_core_coder", true, false)
	for _, value := range []string{"sample_rate: ingest=44100, BRB=48000", "channels: ingest=1, BRB=2"} {
		if !strings.Contains(err.Error(), value) {
			t.Fatal("missing readable audio values", err)
		}
	}
}

func TestBRBMalformedDiagnosticsDoNotInventValuesOrLeakPayloads(t *testing.T) {
	for _, typ := range []uint8{rtmp.Video, rtmp.Audio} {
		s, logs := diagnosticBRB(t, config.BRBProfile{Width: 1920, Height: 1080, FPS: 25, SampleRate: 48000})
		body := []byte{0x17, 0, 0, 0, 0}
		if typ == rtmp.Audio {
			body = []byte{0xaf, 0}
		}
		body = append(body, []byte("private-stream-key-and-peer-error")...)
		if err := s.validateBRBInput(packet(typ, 0, body...)); err == nil {
			t.Fatal("malformed header accepted")
		}
		entry := compatibilityLog(t, logs)
		if strings.Contains(logs.String(), "private-stream-key") {
			t.Fatal("raw payload leaked")
		}
		if entry["compatible"] != false {
			t.Fatal("malformed header marked compatible")
		}
		ingest := entry["ingest"].(map[string]any)
		if _, ok := ingest["fps"]; ok {
			t.Fatal("invented a frame rate for an unreadable header")
		}
	}
}

func TestBRBTimingDiagnosticsDistinguishMissingAndInvalid(t *testing.T) {
	for _, tc := range []struct {
		name   string
		sps    h264.SPS
		want   string
		reject bool
	}{
		{name: "absent", want: "not_signalled"},
		{name: "zero divisor", sps: h264.SPS{VUI: &h264.SPS_VUI{TimingInfo: &h264.SPS_TimingInfo{TimeScale: 50}}}, want: "unavailable_invalid_timing", reject: true},
		{name: "zero time scale", sps: h264.SPS{VUI: &h264.SPS_VUI{TimingInfo: &h264.SPS_TimingInfo{NumUnitsInTick: 1}}}, want: "unavailable_invalid_timing", reject: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			check := &brbInputCheck{ingest: map[string]any{}, brb: map[string]any{}}
			check.timing(&tc.sps, 25)
			if check.ingest["fps"] != tc.want || (len(check.mismatches) > 0) != tc.reject {
				t.Fatalf("wrong timing diagnostic: %+v", check)
			}
			if _, err := json.Marshal(check.ingest); err != nil {
				t.Fatal("non-JSON timing value", err)
			}
		})
	}
}
