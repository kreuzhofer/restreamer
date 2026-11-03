package relay

import (
	"context"
	"errors"
	"sync"
	"time"
)

var errBroadcastCapacity = errors.New("media preparation yielded to continuous output")

const broadcastCapacityMessage = "Waiting for broadcast capacity; rendering restarts from the beginning after Stop."

// preparationGate gives the continuous clock priority over expensive preparation.
// A cancelled attempt must release its processes and scratch files before the
// encoder may start. Only capacity cancellation retries; other failures return.
type preparationGate struct {
	mu        sync.Mutex
	broadcast bool
	active    context.CancelCauseFunc
	changed   chan struct{}
}

func (g *preparationGate) notifyLocked() {
	if g.changed != nil {
		close(g.changed)
	}
	g.changed = make(chan struct{})
}

// reserve is nonblocking so the broadcast controller never waits on render IO.
// Its result confirms all previously admitted preparation has exited.
func (g *preparationGate) reserve(active bool) bool {
	if g == nil {
		return true
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.broadcast != active {
		g.broadcast = active
		if active && g.active != nil {
			g.active(errBroadcastCapacity)
		}
		g.notifyLocked()
	}
	return g.active == nil
}

func (g *preparationGate) run(ctx context.Context, timeout time.Duration, waiting func(), work func(context.Context) error) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if g != nil {
			g.mu.Lock()
			if g.changed == nil {
				g.changed = make(chan struct{})
			}
			if g.broadcast || g.active != nil {
				blocked, changed := g.broadcast, g.changed
				g.mu.Unlock()
				if blocked && waiting != nil {
					waiting()
				}
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-changed:
				}
				continue
			}
		}
		lease, cancel := context.WithCancelCause(ctx)
		if g != nil {
			g.active = cancel
			g.mu.Unlock()
		}
		attempt, stopTimer := context.WithTimeout(lease, timeout)
		err := work(attempt)
		if err == nil {
			err = attempt.Err()
		}
		cause := context.Cause(lease)
		stopTimer()
		cancel(nil)
		if g != nil {
			g.mu.Lock()
			g.active = nil
			g.notifyLocked()
			g.mu.Unlock()
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !errors.Is(cause, errBroadcastCapacity) {
			return err
		}
		if waiting != nil {
			waiting()
		}
	}
}
