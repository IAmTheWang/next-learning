package concurrency101

import (
	"testing"
	"time"
)

func TestFanIn_MergesAllValues(t *testing.T) {
	a := make(chan int)
	b := make(chan int)
	c := make(chan int)

	go func() {
		a <- 1
		a <- 2
		close(a)
	}()
	go func() {
		b <- 3
		close(b)
	}()
	go func() {
		c <- 4
		c <- 5
		close(c)
	}()

	out := FanIn(a, b, c)

	seen := map[int]bool{}
	timeout := time.After(time.Second)
	for i := 0; i < 5; i++ {
		select {
		case v, ok := <-out:
			if !ok {
				t.Fatalf("channel closed early, only got %d values", i)
			}
			seen[v] = true
		case <-timeout:
			t.Fatal("timed out waiting for values")
		}
	}

	for _, want := range []int{1, 2, 3, 4, 5} {
		if !seen[want] {
			t.Fatalf("missing value %d in merged output", want)
		}
	}

	select {
	case _, ok := <-out:
		if ok {
			t.Fatal("expected out channel to be closed after all inputs closed")
		}
	case <-time.After(time.Second):
		t.Fatal("out channel never closed")
	}
}
