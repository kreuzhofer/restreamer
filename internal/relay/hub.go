package relay

import (
	"errors"
	"sync"

	"github.com/kreuzhofer/restreamer/internal/rtmp"
)

const maxQueuePackets = 512

var errCodec = errors.New("v1 requires H.264 video and AAC audio; choose these codecs in OBS")

// A subscription belongs to one connection attempt. Overflow invalidates the
// whole attempt: resuming with arbitrary interframes would produce broken video.
type subscription struct {
	packets       chan *rtmp.Message
	failed        chan struct{}
	waiting       bool
	bytes         int
	broken        bool
	output        *output
	queuedFrames  uint64
	pendingWrites int // Includes the packet currently being written to the socket.
}

type hub struct {
	mu         sync.Mutex
	subs       map[*subscription]bool
	headers    [3]*rtmp.Message // metadata, AVC sequence header, AAC sequence header
	limit      int
	outputs    []*output
	outputSubs map[*output]*subscription
}

func newHub(limit int, outputs ...*output) *hub {
	for _, o := range outputs {
		o.mu.Lock()
		o.failed = false
		o.mu.Unlock()
	}
	return &hub{subs: make(map[*subscription]bool), limit: limit, outputs: outputs, outputSubs: make(map[*output]*subscription)}
}

func (h *hub) subscribe() *subscription {
	return h.subscribeOutput(nil)
}

func (h *hub) subscribeOutput(o *output) *subscription {
	s := &subscription{packets: make(chan *rtmp.Message, maxQueuePackets), failed: make(chan struct{}), waiting: true, output: o}
	h.mu.Lock()
	h.subs[s] = true
	if o != nil {
		h.outputSubs[o] = s
	}
	h.mu.Unlock()
	return s
}

func (h *hub) unsubscribe(s *subscription) { h.finish(s, true) }

func (h *hub) finish(s *subscription, intentional bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.subs[s] {
		return
	}
	delete(h.subs, s)
	if s.output != nil {
		delete(h.outputSubs, s.output)
		s.output.discardFrames(s.queuedFrames, intentional)
	}
	s.queuedFrames = 0
}
func (h *hub) consumed(s *subscription, m *rtmp.Message) {
	h.mu.Lock()
	s.bytes -= len(m.Body)
	if isVideoFrame(m) {
		s.queuedFrames--
	}
	h.mu.Unlock()
}

func (h *hub) delivered(s *subscription) {
	h.mu.Lock()
	s.pendingWrites--
	h.mu.Unlock()
}

func (h *hub) pending(s *subscription) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return s.pendingWrites
}

func (h *hub) offer(s *subscription, m *rtmp.Message) {
	if s.broken {
		if s.output != nil && isVideoFrame(m) {
			s.output.discardFrames(1, false)
		}
		return
	}
	if s.bytes+len(m.Body) <= h.limit {
		select {
		case s.packets <- m:
			s.pendingWrites++
			s.bytes += len(m.Body)
			if isVideoFrame(m) {
				s.queuedFrames++
			}
			return
		default:
		}
	}
	s.broken = true
	if s.output != nil {
		var n uint64
		if isVideoFrame(m) {
			n = 1
		}
		s.output.discardFrames(n, false)
	}
	close(s.failed)
}

func (h *hub) publish(m *rtmp.Message) error {
	header := -1
	isKey := false
	switch m.Type {
	case rtmp.Video:
		if len(m.Body) < 5 || m.Body[0]&0x80 != 0 || m.Body[0]&0x0f != 7 {
			return errCodec
		}
		switch m.Body[1] {
		case 0:
			header = 1
		case 1:
			isKey = m.Body[0]>>4 == 1
		case 2:
		default:
			return errors.New("invalid AVC packet")
		}
	case rtmp.Audio:
		if len(m.Body) < 2 || m.Body[0]>>4 != 10 {
			return errCodec
		}
		if m.Body[1] == 0 {
			header = 2
		} else if m.Body[1] != 1 {
			return errors.New("invalid AAC packet")
		}
	case rtmp.Data:
		// OBS sends onMetaData here. Cache only metadata, not arbitrary timed data.
		if isMetadata(m.Body) {
			header = 0
		}
	default:
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(m.Body) > h.limit {
		return errors.New("media packet exceeds queue_bytes")
	}
	if header >= 0 {
		h.headers[header] = m
	}
	if isVideoFrame(m) {
		for _, o := range h.outputs {
			if h.outputSubs[o] == nil {
				o.unavailableFrame()
			}
		}
	}
	for s := range h.subs {
		if s.output != nil && !s.output.admissionAllowed() {
			if isVideoFrame(m) {
				s.output.discardFrames(1, true)
			}
			continue
		}
		if s.broken {
			if s.output != nil && isVideoFrame(m) {
				s.output.discardFrames(1, false)
			}
			continue
		}
		if s.waiting {
			if !isKey || h.headers[1] == nil {
				if s.output != nil && isVideoFrame(m) {
					s.output.discardFrames(1, true)
				}
				continue
			}
			s.waiting = false
			for _, cached := range h.headers {
				if cached != nil {
					copy := *cached
					copy.Timestamp = m.Timestamp
					h.offer(s, &copy)
				}
			}
		}
		h.offer(s, m)
	}
	return nil
}
