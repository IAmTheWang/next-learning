package concurrency101

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestRunAllWaitGroup_WaitsForEveryTask(t *testing.T) {
	var started int32
	tasks := make([]func() int, 5)
	for i := range tasks {
		i := i
		tasks[i] = func() int {
			atomic.AddInt32(&started, 1)
			time.Sleep(20 * time.Millisecond)
			return i * i
		}
	}

	results := RunAllWaitGroup(tasks)

	if atomic.LoadInt32(&started) != 5 {
		t.Fatalf("expected all 5 tasks to run, got %d", started)
	}
	want := []int{0, 1, 4, 9, 16}
	if len(results) != len(want) {
		t.Fatalf("expected %d results, got %d", len(want), len(results))
	}
	for i := range want {
		if results[i] != want[i] {
			t.Fatalf("result[%d] = %d, want %d (order must match input order)", i, results[i], want[i])
		}
	}
}

func TestRunAllWaitGroup_ActuallyRunsConcurrently(t *testing.T) {
	tasks := make([]func() int, 5)
	for i := range tasks {
		tasks[i] = func() int {
			time.Sleep(100 * time.Millisecond)
			return 0
		}
	}

	start := time.Now()
	RunAllWaitGroup(tasks)
	elapsed := time.Since(start)

	if elapsed > 250*time.Millisecond {
		t.Fatalf("took %v — looks serial, not concurrent (5x100ms tasks should take ~100ms, not ~500ms)", elapsed)
	}
}
