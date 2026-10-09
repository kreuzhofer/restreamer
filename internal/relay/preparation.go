package relay

import "context"

// preparationGate shares one expensive media-preparation slot across generator,
// library and BRB workloads. Live delivery never acquires this gate.
type preparationGate struct{ slot chan struct{} }

func (g *preparationGate) acquire(ctx context.Context) (func(), error) {
	// Standalone library fixtures may prepare directly without a server owner.
	if g == nil {
		return func() {}, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case g.slot <- struct{}{}:
		return func() { <-g.slot }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
