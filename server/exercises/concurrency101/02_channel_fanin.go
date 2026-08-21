package concurrency101

// FanIn merges multiple <-chan int into one. Every value sent on any input
// channel must eventually appear on the returned channel (order across
// channels doesn't matter). Once every input channel is closed, the
// returned channel must be closed too — a caller doing `for v := range out`
// must not block forever.
//
// TODO: implement.
//
// Hints:
//   - start one goroutine per input channel, each forwarding values into
//     a shared out channel
//   - use a sync.WaitGroup to know when every forwarding goroutine is done
//   - start one more goroutine that calls wg.Wait() and then close(out) —
//     it must run after all forwarders, so its own goroutine (not the
//     caller's) is what waits and closes
func FanIn(inputs ...<-chan int) <-chan int {
	panic("TODO: implement me")
}
