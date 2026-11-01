package relay

import (
	"context"
	"sync"
	"time"

	"github.com/kreuzhofer/restreamer/internal/rtmp"
)

type ContinuousStatus struct {
	RequestedSource     string `json:"requested_source"`
	SelectionPending    bool   `json:"selection_pending"`
	Epoch               uint64 `json:"epoch"`
	PreparationDeferred bool   `json:"preparation_deferred"`
	State               string `json:"state"`
	Source              string `json:"source"`
	Error               string `json:"error,omitempty"`
	Restarts            uint64 `json:"restarts"`
	RetryAt             int64  `json:"retry_at,omitempty"`
	DelayMS             *int64 `json:"estimated_delay_ms"`
	Width               int    `json:"width"`
	Height              int    `json:"height"`
	FPS                 int    `json:"fps"`
	SampleRate          int    `json:"sample_rate"`
}

type programIntent struct {
	active, live bool
	source       string
}
type programEvent struct {
	live, end bool
	drain     bool
	epoch     uint64
	message   *rtmp.Message
	at        time.Time
}
type selectedProgramFrame struct {
	source  string
	ingress time.Time
}

// continuousProgram owns one shared codec pipeline. No worker calls back into
// the stage controller: its commands are bounded events and immutable intent.
type continuousProgram struct {
	server                    *Server
	hub                       *hub
	mu                        sync.Mutex
	want                      programIntent
	status                    ContinuousStatus
	events                    chan programEvent
	bytes                     int
	overflow                  bool
	changed                   chan struct{}
	drainRequested, drainDone bool
	drainError                string
	drainDeadline             time.Time
}

func (p *continuousProgram) beginDrain(now time.Time) {
	p.mu.Lock()
	active := p.want.active
	if active {
		p.drainRequested = true
		p.drainDone = false
		p.drainError = ""
		p.drainDeadline = now.Add(10 * time.Second)
	}
	p.mu.Unlock()
	if active {
		p.offer(programEvent{drain: true})
	}
}

func (p *continuousProgram) drainState(now time.Time) (bool, bool, string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.drainRequested && !p.drainDone && !now.Before(p.drainDeadline) {
		return true, true, "encoder_drain_deadline_exceeded"
	}
	return p.drainRequested, p.drainDone, p.drainError
}

func newContinuousProgram(s *Server) *continuousProgram {
	return &continuousProgram{server: s, hub: newHub(s.cfg.QueueBytes, s.pathOutputs("continuous")...), events: make(chan programEvent, 512), changed: make(chan struct{}), status: ContinuousStatus{State: "off", Source: "off", Width: 1920, Height: 1080, FPS: 25, SampleRate: 48000}}
}

func (p *continuousProgram) snapshot() ContinuousStatus {
	p.mu.Lock()
	defer p.mu.Unlock()
	status := p.status
	status.RequestedSource = p.want.source
	if p.want.live {
		status.RequestedSource = "obs"
	}
	if !p.want.active {
		status.RequestedSource = "off"
	}
	status.SelectionPending = p.want.active && status.Source != status.RequestedSource
	return status
}

func (p *continuousProgram) offer(e programEvent) {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := 0
	if e.message != nil {
		n = len(e.message.Body)
	}
	if p.bytes+n > 16<<20 {
		p.overflow = true
		return
	}
	select {
	case p.events <- e:
		p.bytes += n
	default:
		p.overflow = true
	}
}

func (b *broadcast) syncContinuousLocked() {
	configured := false
	for _, o := range b.server.outputSnapshot() {
		if o.config.Mode() == "continuous" {
			configured = true
			break
		}
	}
	source := "brb"
	if b.clip != nil && !b.manual && !b.clip.paused {
		source = "file"
	}
	if b.fallbackFailed && b.active {
		source = "emergency"
	}
	b.continuous.mu.Lock()
	b.continuous.want = programIntent{active: configured && b.control.stage != "OFF", live: b.control.stage == "LIVE" && !b.manual && b.clip == nil, source: source}
	active := b.continuous.want.active
	b.continuous.status.PreparationDeferred = active
	b.continuous.mu.Unlock()
	if active {
		b.server.preparation.reserve(true)
	}
}

func (b *broadcast) emitPrepared(m *rtmp.Message, ts time.Duration) {
	copy := *m
	copy.Timestamp = ts
	b.continuous.offer(programEvent{epoch: b.previewEpoch, message: &copy, at: time.Now()})
	b.emit(m, ts)
}

func programHeaderIndex(m *rtmp.Message) int {
	if m.Type == rtmp.Data && isMetadata(m.Body) {
		return 0
	}
	if len(m.Body) > 1 && m.Body[1] == 0 {
		if m.Type == rtmp.Video {
			return 1
		}
		if m.Type == rtmp.Audio {
			return 2
		}
	}
	return -1
}

func (p *continuousProgram) invalidateOutput() {
	p.hub.mu.Lock()
	p.hub.headers = [3]*rtmp.Message{}
	for sub := range p.hub.subs {
		if !sub.broken {
			sub.broken = true
			close(sub.failed)
		}
	}
	p.hub.mu.Unlock()
	p.mu.Lock()
	p.status.Epoch++
	close(p.changed)
	p.changed = make(chan struct{})
	p.mu.Unlock()
}

func (p *continuousProgram) run(ctx context.Context) {
	var live, prepared *programDecoder
	var encoder *programEncoder
	var liveHeaders, preparedHeaders [3]*rtmp.Message
	var epoch uint64
	inputEnded := true
	var lastLive, lastPrepared *programFrame
	var lastGood, preparedGood time.Time
	var retry time.Time
	backoff := time.Second
	draining, flushing := false, false
	silence := make([]byte, continuousAudioBytes)
	var emergency *programFrame
	selected := make(chan selectedProgramFrame, 16)
	closeSources := func() {
		if live != nil {
			live.close()
			live = nil
		}
		if prepared != nil {
			prepared.close()
			prepared = nil
		}
		lastLive, lastPrepared = nil, nil
		inputEnded = true
	}
	stop := func() {
		if encoder != nil {
			encoder.close()
			encoder = nil
		}
		closeSources()
		p.invalidateOutput()
		for len(selected) > 0 {
			<-selected
		}
	}
	defer func() { stop(); p.server.preparation.reserve(false) }()
	ticker := time.NewTicker(continuousFrameTime)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case e := <-p.events:
			p.mu.Lock()
			if e.message != nil {
				p.bytes -= len(e.message.Body)
			}
			want := p.want
			p.mu.Unlock()
			if e.drain {
				draining = true
				if prepared != nil {
					prepared.end()
				}
				continue
			}
			if e.end {
				inputEnded = true
				liveHeaders = [3]*rtmp.Message{}
				if live != nil {
					live.end()
				}
				continue
			}
			m := e.message
			if m == nil {
				continue
			}
			headers := &preparedHeaders
			decoder := &prepared
			if e.live {
				headers = &liveHeaders
				decoder = &live
			} else if e.epoch != epoch {
				epoch = e.epoch
				preparedHeaders = [3]*rtmp.Message{}
				if prepared != nil {
					prepared.close()
					prepared = nil
				}
				lastPrepared = nil
			}
			if h := programHeaderIndex(m); h >= 0 {
				headers[h] = m
			}
			key := isVideoFrame(m) && m.Body[0]>>4 == 1
			dead := *decoder != nil && (*decoder).ctx.Err() != nil
			if want.active && p.server.preparation.reserve(true) && key && headers[1] != nil && headers[2] != nil && (*decoder == nil || dead || e.live && inputEnded) {
				if *decoder != nil {
					(*decoder).close()
					*decoder = nil
				}
				d, err := startProgramDecoder(ctx, m.Timestamp)
				if err != nil {
					p.mu.Lock()
					p.status.Error = "Continuous source decoder unavailable; check FFmpeg"
					p.mu.Unlock()
					continue
				}
				*decoder = d
				for _, h := range headers {
					if h != nil {
						d.feed(h, e.at)
					}
				}
				if e.live {
					inputEnded = false
				}
			}
			if want.active && *decoder != nil {
				(*decoder).feed(m, e.at)
			}
		case now := <-ticker.C:
			p.mu.Lock()
			want, overflow := p.want, p.overflow
			p.overflow = false
			p.mu.Unlock()
			if !want.active {
				if encoder != nil || live != nil || prepared != nil {
					stop()
				}
				p.server.preparation.reserve(false)
				p.mu.Lock()
				p.status.State, p.status.Source, p.status.RetryAt, p.status.DelayMS = "off", "off", 0, nil
				p.status.Error = ""
				p.drainRequested, p.drainDone = false, false
				p.mu.Unlock()
				draining, flushing = false, false
				retry = time.Time{}
				backoff = time.Second
				continue
			}
			if !p.server.preparation.reserve(true) {
				p.mu.Lock()
				p.status.State, p.status.Source = "waiting_capacity", "unavailable"
				p.status.DelayMS = nil
				p.mu.Unlock()
				continue
			}
			if emergency == nil {
				emergency = continuousEmergencyFrame()
			}
			if overflow {
				closeSources()
				p.mu.Lock()
				p.status.Error = "Source queue exceeded its bound; waiting for a fresh keyframe"
				p.mu.Unlock()
			}
			if encoder != nil {
				select {
				case <-encoder.done:
					if draining {
						encoder.mu.Lock()
						err := encoder.err
						encoder.mu.Unlock()
						encoder.close()
						encoder = nil
						p.mu.Lock()
						p.drainDone = true
						if err != nil {
							p.drainError = "encoder_failed_during_drain"
						}
						p.mu.Unlock()
						flushing = true
						continue
					}
					encoder.close()
					encoder = nil
					p.invalidateOutput()
					for len(selected) > 0 {
						<-selected
					}
					retry = now.Add(backoff)
					backoff = min(30*time.Second, backoff*2)
					p.mu.Lock()
					p.status.State = "recovering"
					p.status.Source = "unavailable"
					p.status.Error = "Continuous encoder stopped; retrying without changing output mode"
					p.status.DelayMS = nil
					p.status.Restarts++
					p.status.RetryAt = retry.UnixMilli()
					p.mu.Unlock()
				default:
				}
			}
			if flushing {
				continue
			}
			if encoder == nil {
				if now.Before(retry) {
					continue
				}
				var err error
				encoder, err = startProgramEncoder(ctx, func(m *rtmp.Message) error {
					if isVideoFrame(m) {
						select {
						case frame := <-selected:
							p.mu.Lock()
							p.status.State, p.status.Source, p.status.RetryAt = "running", frame.source, 0
							p.status.DelayMS = nil
							if frame.source == "obs" || frame.source == "file" || frame.source == "brb" {
								p.status.Error = ""
							}
							if !frame.ingress.IsZero() {
								delay := time.Since(frame.ingress).Milliseconds()
								p.status.DelayMS = &delay
							}
							p.mu.Unlock()
						default:
						}
					}
					return p.hub.publish(m)
				})
				if err != nil {
					retry = now.Add(backoff)
					backoff = min(30*time.Second, backoff*2)
					p.mu.Lock()
					p.status.State = "failed"
					p.status.Error = err.Error()
					p.status.RetryAt = retry.UnixMilli()
					p.mu.Unlock()
					continue
				}
				p.mu.Lock()
				p.status.State = "starting"
				p.mu.Unlock()
			}
			if draining && (prepared == nil || prepared.drained()) {
				encoder.finish()
				flushing = true
				continue
			}
			var frame *programFrame
			if live != nil {
				frame = live.next(now, 4*time.Second)
				if frame != nil {
					lastLive, lastGood = frame, now
				}
			}
			if prepared != nil {
				if f := prepared.next(now, 100*time.Millisecond); f != nil {
					lastPrepared, preparedGood = f, now
				}
			}
			source := want.source
			fallback := emergency
			if lastPrepared != nil && now.Sub(preparedGood) <= continuousFrameTime {
				fallback = lastPrepared
			} else if lastPrepared != nil && now.Sub(preparedGood) <= time.Second {
				fallback = &programFrame{video: lastPrepared.video, audio: silence}
				source = "held"
			} else {
				source = "emergency"
			}
			if want.live && frame != nil {
				source = "obs"
			} else if want.live && lastLive != nil && now.Sub(lastGood) <= time.Second {
				frame = &programFrame{video: lastLive.video, audio: silence}
				source = "held"
			} else {
				frame = fallback
			}
			if !want.live {
				frame = fallback
			}
			ingress := time.Time{}
			if source == "obs" {
				ingress = frame.ingress
			}
			select {
			case selected <- selectedProgramFrame{source, ingress}:
			default:
				encoder.cancel()
				continue
			}
			if !encoder.put(frame) {
				encoder.cancel()
			}
		}
	}
}
