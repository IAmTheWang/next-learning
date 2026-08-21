package concurrency101

// Counter as written below has a data race: count++ is read-modify-write,
// not atomic, so concurrent calls to Inc() can step on each other and lose
// increments.
//
// TODO: add a sync.Mutex field and lock/unlock it in both Inc() and
// Value() to fix the race.
type Counter struct {
	count int
}

func (c *Counter) Inc() {
	c.count++
}

func (c *Counter) Value() int {
	return c.count
}
