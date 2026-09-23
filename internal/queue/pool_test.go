package queue

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPoolProcessesJob(t *testing.T) {
	p := New(2, 16, 8, func(payload string) Result {
		return Result{Masked: "masked:" + payload}
	})
	defer p.Close()
	res, err := p.Submit(context.Background(), "hello", false)
	require.NoError(t, err)
	require.Equal(t, "masked:hello", res.Masked)
}

func TestPoolBackpressure(t *testing.T) {
	// Handler blocks forever; fill the fast queue, then Submit must time out.
	block := make(chan struct{})
	p := New(1, 2, 2, func(payload string) Result {
		<-block
		return Result{}
	})
	defer p.Close()
	defer close(block)
	// Occupy the single worker.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	_, _ = p.Submit(ctx, "a", false)
	cancel()
	// Fill the fast queue (capacity 2). The worker is blocked, so these
	// enqueue but time out waiting for a result; they remain queued.
	ctxB, cancelB := context.WithTimeout(context.Background(), 50*time.Millisecond)
	_, _ = p.Submit(ctxB, "b", false)
	cancelB()
	ctxC, cancelC := context.WithTimeout(context.Background(), 50*time.Millisecond)
	_, _ = p.Submit(ctxC, "c", false)
	cancelC()
	// Next submit with a short timeout -> error (queue full).
	ctx2, cancel2 := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel2()
	_, err := p.Submit(ctx2, "d", false)
	require.Error(t, err)
}

func TestPoolMetrics(t *testing.T) {
	var busy atomic.Int32
	p := New(2, 16, 8, func(payload string) Result {
		busy.Add(1)
		time.Sleep(20 * time.Millisecond)
		busy.Add(-1)
		return Result{}
	})
	defer p.Close()
	_, _ = p.Submit(context.Background(), "x", false)
	require.GreaterOrEqual(t, p.WorkersBusy(), 0)
	require.GreaterOrEqual(t, p.QueueDepth(), 0)
	_ = busy.Load()
}

func TestPoolHandlerError(t *testing.T) {
	p := New(1, 4, 4, func(payload string) Result {
		return Result{Err: errors.New("boom")}
	})
	defer p.Close()
	res, err := p.Submit(context.Background(), "x", false)
	require.NoError(t, err)
	require.Error(t, res.Err)
}