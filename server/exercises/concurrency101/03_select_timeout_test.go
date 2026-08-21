package concurrency101

import (
	"testing"
	"time"
)

func TestFirstResult_ReturnsFastestWorker(t *testing.T) {
	slow := func() int {
		time.Sleep(200 * time.Millisecond)
		return 1
	}
	fast := func() int {
		time.Sleep(10 * time.Millisecond)
		return 2
	}

	start := time.Now()
	result, ok := FirstResult(time.Second, slow, fast)
	elapsed := time.Since(start)

	if !ok {
		t.Fatal("expected a result, got timeout")
	}
	if result != 2 {
		t.Fatalf("expected fastest worker's result (2), got %d", result)
	}
	if elapsed > 100*time.Millisecond {
		t.Fatalf("took %v, should return as soon as the fast worker finishes (~10ms), not wait for the slow one", elapsed)
	}
}

func TestFirstResult_TimesOutIfAllWorkersAreSlow(t *testing.T) {
	slow := func() int {
		time.Sleep(200 * time.Millisecond)
		return 1
	}

	start := time.Now()
	_, ok := FirstResult(30*time.Millisecond, slow, slow)
	elapsed := time.Since(start)

	if ok {
		t.Fatal("expected timeout (ok=false), got a result")
	}
	if elapsed > 100*time.Millisecond {
		t.Fatalf("took %v, should give up at ~30ms timeout, not wait for workers", elapsed)
	}
}
