package relay

import (
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/kreuzhofer/restreamer/internal/config"
	"github.com/kreuzhofer/restreamer/internal/rtmp"
)

func TestGoLiveRejectsMalformedAVCBeforeCommittingFreshPreparedKeyframe(t *testing.T) {
	index, path := preparedClip(t)
	reader, err := openClip(path, index)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.close()
	var valid *rtmp.Message
	for {
		message, err := reader.next()
		if err != nil {
			t.Fatal(err)
		}
		if isVideoFrame(message) && message.Body[0]>>4 == 1 {
			valid = message
			break
		}
	}
	sink := newSink(t)
	s, address := startRelayWithLogger(t, []config.Target{sink.target("one")}, slog.New(slog.NewTextHandler(io.Discard, nil)), true)
	if w := stageRequest(t, s, "go_live", nil); w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	publisher := publishInput(t, address)
	writePacket(t, publisher, packet(rtmp.Video, 0, 0x17, 0, 0, 0, 0, 1, 0x64, 0, 0x1f))
	writePacket(t, publisher, valid)
	eventually(t, func() bool { return s.inputFrames.Load() == 1 })
	if state := readStage(t, s); state.Stage != "OFF" || state.Pending == nil || sink.count() != 0 {
		t.Fatal("malformed AVC configuration activated delivery", state)
	}
	writePacket(t, publisher, index.Video)
	for i, frame := range []*rtmp.Message{
		packet(rtmp.Video, time.Second, 0x17, 1, 0, 0, 0, 0, 0, 0, 16, 0x65, 0x88),
		packet(rtmp.Video, 2*time.Second, 0x17, 1, 0, 0, 0, 0, 0, 0, 2, 0x41, 0x88),
	} {
		writePacket(t, publisher, frame)
		eventually(t, func() bool { return s.inputFrames.Load() == uint64(i+2) })
		if state := readStage(t, s); state.Stage != "OFF" || state.Pending == nil || sink.count() != 0 {
			t.Fatal("malformed or non-IDR frame activated delivery", state)
		}
	}
	writePacket(t, publisher, valid)
	eventually(t, func() bool { return readStage(t, s).Stage == "LIVE" })
	eventually(t, func() bool { return sink.count() == 1 })
	// A later fresh sample reaches the newly joined destination with its valid
	// initialization header; no re-encoding is involved.
	valid.Timestamp = 3 * time.Second
	writePacket(t, publisher, valid)
	if got := receive(t, sink); got.Type != rtmp.Video || len(got.Body) != len(index.Video.Body) {
		t.Fatal("valid source did not bootstrap destination")
	}
	eventually(t, func() bool { return s.inputFrames.Load() == 5 })
	publisher.Net.Close()
	eventually(t, func() bool { return readStage(t, s).Source == "off" })
	reconnected := publishInput(t, address)
	writePacket(t, reconnected, index.Video)
	writePacket(t, reconnected, packet(rtmp.Video, 0, 0x17, 1, 0, 0, 0, 0, 0, 0, 16, 0x65, 0x88))
	eventually(t, func() bool { return s.inputFrames.Load() == 6 })
	if state := readStage(t, s); state.Source != "off" || state.Stage != "LIVE" {
		t.Fatal("malformed recovery frame was put on air", state)
	}
	writePacket(t, reconnected, valid)
	eventually(t, func() bool { return readStage(t, s).Source == "obs" })
	if sink.count() != 1 {
		t.Fatal("input recovery reconnected destination")
	}
}
