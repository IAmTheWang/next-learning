package db

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Connect retries because docker compose's Postgres container is often still
// starting up when this process boots; failing fast on the first attempt
// would crash the service on every fresh `docker compose up`.
func Connect(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	const maxAttempts = 5
	var lastErr error

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		pool, err := pgxpool.New(ctx, databaseURL)
		if err == nil {
			pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			err = pool.Ping(pingCtx)
			cancel()
			if err == nil {
				return pool, nil
			}
			pool.Close()
		}

		lastErr = err
		log.Printf("db connect attempt %d/%d failed: %v", attempt, maxAttempts, err)
		time.Sleep(1 * time.Second)
	}

	return nil, fmt.Errorf("could not connect to database after %d attempts: %w", maxAttempts, lastErr)
}
