package concurrency101

import (
	"sync"
	"testing"
)

// Run with -race to actually see the problem before you add the mutex:
//   go test -race ./exercises/concurrency101/... -run TestCounter -v
func TestCounter_ConcurrentIncrements(t *testing.T) {
	c := &Counter{}
	const goroutines = 100
	const incsPerGoroutine = 1000

	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < incsPerGoroutine; j++ {
				c.Inc()
			}
		}()
	}
	wg.Wait()

	want := goroutines * incsPerGoroutine
	if got := c.Value(); got != want {
		t.Fatalf("Value() = %d, want %d — lost increments means the race is still there", got, want)
	}
}
