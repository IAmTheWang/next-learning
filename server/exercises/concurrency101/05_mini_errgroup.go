package concurrency101

import "context"

// MiniGroup is a stripped-down rebuild of golang.org/x/sync/errgroup.Group.
// The point isn't to use this instead of the real thing — it's that writing
// it yourself is what makes it obvious exactly what
// errgroup.WithContext(ctx) in ../../internal/bff/aggregate.go is doing for
// you under the hood.
//
// TODO: implement NewMiniGroup, (*MiniGroup).Go, and (*MiniGroup).Wait.
//
// Behavior to match:
//   - NewMiniGroup(ctx) returns a *MiniGroup and a derived context that gets
//     cancelled the moment any task passed to Go reports an error
//   - Go(fn) starts fn in its own goroutine; if fn returns a non-nil error,
//     record it (only if no error has been recorded yet) and cancel the
//     derived context
//   - Wait() blocks until every task started via Go has finished, then
//     returns the first recorded error (nil if none)
//
// Hints:
//   - fields you'll need: a sync.WaitGroup, a sync.Mutex + a stored error
//     to protect "first error wins", and the cancel func from
//     context.WithCancel
//   - only the *first* error matters — later ones can just be dropped, this
//     matches what real errgroup does
type MiniGroup struct {
	// TODO: add fields
}

func NewMiniGroup(ctx context.Context) (*MiniGroup, context.Context) {
	panic("TODO: implement me")
}

func (g *MiniGroup) Go(fn func() error) {
	panic("TODO: implement me")
}

func (g *MiniGroup) Wait() error {
	panic("TODO: implement me")
}
