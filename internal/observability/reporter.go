package observability

import (
	"context"
	"time"

	"github.com/kind-earthquake/pii-module/internal/queue"
	"github.com/kind-earthquake/pii-module/internal/store"
)

// StartReporter periodically publishes pool and store health to Prometheus
// gauges. It runs until ctx is cancelled.
func StartReporter(ctx context.Context, pool *queue.Pool, layered *store.LayeredStore, interval time.Duration) {
	if interval <= 0 {
		interval = time.Second
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if pool != nil {
					QueueDepth.Set(float64(pool.QueueDepth()))
					WorkersBusy.Set(float64(pool.WorkersBusy()))
				}
				if layered != nil {
					if layered.RedisUp() {
						RedisUp.Set(1)
					} else {
						RedisUp.Set(0)
					}
				}
			}
		}
	}()
}