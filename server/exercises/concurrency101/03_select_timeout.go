package concurrency101

import "time"

// FirstResult runs every worker concurrently and returns the result of
// whichever one finishes first, along with true. If no worker finishes
// within timeout, it returns (0, false).
//
// This is a simplified version of what errgroup.WithContext does under the
// hood: race several goroutines, and stop waiting as soon as either a
// winner or a deadline shows up.
//
// TODO: implement using select.
//
// Hints:
//   - start every worker in its own goroutine, writing its result into a
//     shared results channel
//   - make results buffered with capacity len(workers): if you select the
//     timeout branch first, the other (slower) workers must still be able
//     to send their result without blocking forever once nobody is
//     receiving anymore — an unbuffered channel here leaks goroutines
//     (this is exactly the Q3 leak scenario documented in NOTES.md)
//   - use select on `case r := <-results:` vs `case <-time.After(timeout):`
func FirstResult(timeout time.Duration, workers ...func() int) (int, bool) {
	panic("TODO: implement me")
}
