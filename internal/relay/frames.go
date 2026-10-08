package relay

import "github.com/kreuzhofer/restreamer/internal/rtmp"

// Standard OBS AVC media packets contain one video frame. Count packets without
// decoding H.264; exclude codec configuration, end markers, audio and metadata.
// These transport statistics cannot measure OBS rendering or remote playback.
func isVideoFrame(m *rtmp.Message) bool {
	return m.Type == rtmp.Video && len(m.Body) > 5 && m.Body[0]&0x80 == 0 && m.Body[0]&0x0f == 7 && m.Body[1] == 1
}

func (o *output) recordSent(m *rtmp.Message) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if m.Type == rtmp.Video || m.Type == rtmp.Audio {
		o.status.Bytes += uint64(len(m.Body))
	}
	if isVideoFrame(m) {
		o.status.Frames++
		o.failed = false
	}
	if o.status.Enabled && !o.blocked {
		o.status.State = "streaming"
	}
}

func (o *output) discardFrames(n uint64, intentional bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	switch {
	case !o.status.Enabled || o.blocked:
		o.status.PausedFrames += n
	case intentional:
		o.status.SkippedFrames += n
	default:
		o.status.DroppedFrames += n
		o.failed = true
	}
}

// Frames arriving before the first connection (or after a manual resume) are
// startup skips. After an error, missing connections are part of that outage.
func (o *output) unavailableFrame() {
	o.mu.Lock()
	defer o.mu.Unlock()
	switch {
	case !o.status.Enabled || o.blocked:
		o.status.PausedFrames++
	case o.failed:
		o.status.DroppedFrames++
	default:
		o.status.SkippedFrames++
	}
}
