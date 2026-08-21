// Package bff implements the concurrent-fan-out aggregation pattern typical
// of a BFF (Backend-For-Frontend) layer: call several upstream services for
// one incoming request, and return once they've all answered (or one of
// them fails / the whole thing times out).
package bff

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"golang.org/x/sync/errgroup"
)

// Upstreams holds the endpoint for each downstream service this BFF fans
// out to.
type Upstreams struct {
	User  string
	Order string
	Stock string
}

// Result holds the raw JSON payload returned by each upstream.
type Result struct {
	User  json.RawMessage
	Order json.RawMessage
	Stock json.RawMessage
}

func fetch(ctx context.Context, client *http.Client, url string) (json.RawMessage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch %s: unexpected status %d", url, resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: read body: %w", url, err)
	}
	return body, nil
}

// safeGo wraps an errgroup task with panic recovery. errgroup does not
// recover panics on its own — an unrecovered panic in one upstream call
// would crash the whole process, not just fail that one call.
func safeGo(g *errgroup.Group, fn func() error) {
	g.Go(func() (err error) {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("panic recovered: %v", r)
			}
		}()
		return fn()
	})
}

// Aggregate fans out to the three upstreams concurrently via errgroup and
// waits for all of them, or for the first failure/timeout to cancel the
// rest. Overall latency is bounded by the slowest upstream (max(Ti)),
// not the sum of all three (ΣTi) as a serial call chain would be.
func Aggregate(parent context.Context, client *http.Client, timeout time.Duration, up Upstreams) (*Result, error) {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	g, ctx := errgroup.WithContext(ctx)
	result := &Result{}

	safeGo(g, func() error {
		data, err := fetch(ctx, client, up.User)
		if err != nil {
			return err
		}
		result.User = data
		return nil
	})
	safeGo(g, func() error {
		data, err := fetch(ctx, client, up.Order)
		if err != nil {
			return err
		}
		result.Order = data
		return nil
	})
	safeGo(g, func() error {
		data, err := fetch(ctx, client, up.Stock)
		if err != nil {
			return err
		}
		result.Stock = data
		return nil
	})

	if err := g.Wait(); err != nil {
		return nil, fmt.Errorf("aggregation failed: %w", err)
	}
	return result, nil
}

// AggregateSerial is the naive baseline — the same three calls, one after
// another. It exists only so AggregateSerial and Aggregate can be
// benchmarked side by side to get a real, measured speedup number instead
// of an assumed one.
func AggregateSerial(parent context.Context, client *http.Client, timeout time.Duration, up Upstreams) (*Result, error) {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	result := &Result{}
	var err error

	if result.User, err = fetch(ctx, client, up.User); err != nil {
		return nil, fmt.Errorf("aggregation failed: %w", err)
	}
	if result.Order, err = fetch(ctx, client, up.Order); err != nil {
		return nil, fmt.Errorf("aggregation failed: %w", err)
	}
	if result.Stock, err = fetch(ctx, client, up.Stock); err != nil {
		return nil, fmt.Errorf("aggregation failed: %w", err)
	}
	return result, nil
}
