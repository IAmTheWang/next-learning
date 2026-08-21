package bff

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/sync/errgroup"
)

func mockUpstream(delay time.Duration, body string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(delay)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(body))
	}))
}

func testUpstreams(delay time.Duration) (Upstreams, func()) {
	user := mockUpstream(delay, `{"id":1,"name":"user"}`)
	order := mockUpstream(delay, `{"id":2,"item":"order"}`)
	stock := mockUpstream(delay, `{"id":3,"qty":10}`)
	up := Upstreams{User: user.URL, Order: order.URL, Stock: stock.URL}
	cleanup := func() {
		user.Close()
		order.Close()
		stock.Close()
	}
	return up, cleanup
}

func TestAggregate_ReturnsAllThree(t *testing.T) {
	up, cleanup := testUpstreams(10 * time.Millisecond)
	defer cleanup()

	result, err := Aggregate(context.Background(), http.DefaultClient, time.Second, up)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var user map[string]any
	if err := json.Unmarshal(result.User, &user); err != nil {
		t.Fatalf("bad user payload: %v", err)
	}
	if result.Order == nil || result.Stock == nil {
		t.Fatalf("expected all three payloads, got order=%s stock=%s", result.Order, result.Stock)
	}
}

// TestAggregate_TimeoutCancelsSiblings proves the "one slow upstream can't
// drag the whole request down" claim: with a 20ms budget against 200ms-slow
// upstreams, Aggregate must fail fast via context cancellation, not wait
// out the full 200ms.
func TestAggregate_TimeoutCancelsSiblings(t *testing.T) {
	up, cleanup := testUpstreams(200 * time.Millisecond)
	defer cleanup()

	start := time.Now()
	_, err := Aggregate(context.Background(), http.DefaultClient, 20*time.Millisecond, up)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if elapsed > 100*time.Millisecond {
		t.Fatalf("expected fast failure via context cancellation, took %v", elapsed)
	}
}

// TestSafeGoRecoversPanic exercises safeGo directly: a panic inside one
// upstream goroutine must come back as an error from g.Wait(), not crash
// the test process. (A panic inside an httptest handler is recovered by
// net/http itself before it ever reaches our client, so that path can't
// exercise safeGo — this is the only way to actually test it.)
func TestSafeGoRecoversPanic(t *testing.T) {
	g, _ := errgroup.WithContext(context.Background())
	safeGo(g, func() error {
		panic("boom")
	})
	err := g.Wait()
	if err == nil {
		t.Fatal("expected panic to be converted to an error")
	}
	if !strings.Contains(err.Error(), "panic recovered") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func BenchmarkAggregateSerial(b *testing.B) {
	up, cleanup := testUpstreams(150 * time.Millisecond)
	defer cleanup()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := AggregateSerial(context.Background(), http.DefaultClient, time.Second, up); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkAggregateParallel(b *testing.B) {
	up, cleanup := testUpstreams(150 * time.Millisecond)
	defer cleanup()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Aggregate(context.Background(), http.DefaultClient, time.Second, up); err != nil {
			b.Fatal(err)
		}
	}
}
