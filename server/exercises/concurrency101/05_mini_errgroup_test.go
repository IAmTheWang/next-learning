package concurrency101

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestMiniGroup_WaitsForAllOnSuccess(t *testing.T) {
	g, _ := NewMiniGroup(context.Background())
	var ran [3]bool

	g.Go(func() error { ran[0] = true; return nil })
	g.Go(func() error { ran[1] = true; return nil })
	g.Go(func() error { ran[2] = true; return nil })

	if err := g.Wait(); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	for i, r := range ran {
		if !r {
			t.Fatalf("task %d never ran", i)
		}
	}
}

func TestMiniGroup_ReturnsFirstError(t *testing.T) {
	g, _ := NewMiniGroup(context.Background())
	boom := errors.New("boom")

	g.Go(func() error { return nil })
	g.Go(func() error { return boom })

	if err := g.Wait(); !errors.Is(err, boom) {
		t.Fatalf("expected boom error, got %v", err)
	}
}

func TestMiniGroup_CancelsContextOnFirstError(t *testing.T) {
	g, ctx := NewMiniGroup(context.Background())
	boom := errors.New("boom")

	g.Go(func() error {
		time.Sleep(20 * time.Millisecond)
		return boom
	})
	g.Go(func() error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
			return errors.New("ctx was never cancelled — sibling task didn't get cancelled")
		}
	})

	if err := g.Wait(); err == nil {
		t.Fatal("expected an error, got nil")
	}
}
