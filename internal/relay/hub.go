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
	packets chan *rtmp.Message
	failed  chan struct{}
	waiting bool
	bytes   int
	broken  bool
}

type hub struct {
	mu      sync.Mutex
	subs    map[*subscription]bool
	headers [3]*rtmp.Message // metadata, AVC sequence header, AAC sequence header
	limit   int
}

func newHub(limit int) *hub { return &hub{subs: make(map[*subscription]bool), limit: limit} }

func (h *hub) subscribe() *subscription {
	s := &subscription{packets: make(chan *rtmp.Message, maxQueuePackets), failed: make(chan struct{}), waiting: true}
	h.mu.Lock()
	h.subs[s] = true
	h.mu.Unlock()
	return s
}

func (h *hub) unsubscribe(s *subscription) { h.mu.Lock(); delete(h.subs, s); h.mu.Unlock() }
func (h *hub) consumed(s *subscription, m *rtmp.Message) {
	h.mu.Lock()
	s.bytes -= len(m.Body)
	h.mu.Unlock()
}

func (h *hub) offer(s *subscription, m *rtmp.Message) {
	if s.broken {
		return
	}
	if s.bytes+len(m.Body) <= h.limit {
		select {
		case s.packets <- m:
			s.bytes += len(m.Body)
			return
		default:
		}
	}
	s.broken = true
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
	for s := range h.subs {
		if s.broken {
			continue
		}
		if s.waiting {
			if !isKey || h.headers[1] == nil {
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
