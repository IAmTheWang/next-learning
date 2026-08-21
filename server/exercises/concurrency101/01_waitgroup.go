package concurrency101

// RunAllWaitGroup runs every task in tasks concurrently, waits for all of
// them to finish, and returns their results in the same order as the input
// slice (task i's result goes at index i, even though the tasks finish in
// whatever order the scheduler picks).
//
// TODO: implement using sync.WaitGroup.
//
// Hints:
//   - call wg.Add(1) before starting each goroutine, not inside it
//   - defer wg.Done() inside each goroutine
//   - write results[i] = task() directly by index instead of append — each
//     goroutine owns a different index, so there's no concurrent write to
//     the same memory and no mutex is needed here
func RunAllWaitGroup(tasks []func() int) []int {
	panic("TODO: implement me")

}
