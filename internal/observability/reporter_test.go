package observability

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	"github.com/kind-earthquake/pii-module/internal/queue"
)

func TestReporterSetsGauges(t *testing.T) {
	p := queue.New(2, 8, 4, func(system, payload string) queue.Result { return queue.Result{} })
	defer p.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	StartReporter(ctx, p, nil, 10*time.Millisecond)
	time.Sleep(30 * time.Millisecond)
	require.GreaterOrEqual(t, testutil.ToFloat64(QueueDepth), float64(0))
	require.GreaterOrEqual(t, testutil.ToFloat64(WorkersBusy), float64(0))
}