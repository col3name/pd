package queue

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
)

// ErrQueueFull is returned when both queues are full and the context expires.
var ErrQueueFull = errors.New("queue full")

// Job is a unit of work submitted to the pool.
type Job struct {
	System  string
	Payload string
	Heavy   bool
	done    chan Result
}

// Result is the outcome of processing a Job.
type Result struct {
	Masked string
	Types  []string
	Tokens map[string]string
	Err    error
}

// Handler processes a system's payload into a Result.
type Handler func(system, payload string) Result

// Pool runs a fixed number of workers consuming from two queues: fast (short
// payloads, low latency) and heavy (long payloads). Submit blocks until a
// worker produces a result or the context expires.
type Pool struct {
	fast  chan Job
	heavy chan Job
	done  chan struct{}
	wg    sync.WaitGroup
	busy  atomic.Int32
}

// New returns a Pool with `workers` goroutines and the given queue capacities.
func New(workers, fastCap, heavyCap int, h Handler) *Pool {
	if workers <= 0 {
		workers = 1
	}
	p := &Pool{
		fast:  make(chan Job, fastCap),
		heavy: make(chan Job, heavyCap),
		done:  make(chan struct{}),
	}
	for i := 0; i < workers; i++ {
		p.wg.Add(1)
		go p.worker(h)
	}
	return p
}

func (p *Pool) worker(h Handler) {
	defer p.wg.Done()
	for {
		select {
		case <-p.done:
			return
		case j := <-p.fast:
			p.run(h, j)
		case j := <-p.heavy:
			p.run(h, j)
		}
	}
}

func (p *Pool) run(h Handler, j Job) {
	p.busy.Add(1)
	res := h(j.System, j.Payload)
	p.busy.Add(-1)
	j.done <- res
}

// Submit enqueues a job and waits for its result. It returns ErrQueueFull if
// the context expires before a worker picks up the job.
func (p *Pool) Submit(ctx context.Context, system, payload string, heavy bool) (Result, error) {
	j := Job{System: system, Payload: payload, Heavy: heavy, done: make(chan Result, 1)}
	select {
	case <-ctx.Done():
		return Result{}, ErrQueueFull
	default:
	}
	if heavy {
		select {
		case p.heavy <- j:
		case <-ctx.Done():
			return Result{}, ErrQueueFull
		}
	} else {
		select {
		case p.fast <- j:
		case <-ctx.Done():
			return Result{}, ErrQueueFull
		}
	}
	select {
	case res := <-j.done:
		return res, nil
	case <-ctx.Done():
		return Result{}, ErrQueueFull
	}
}

// QueueDepth returns the total number of queued jobs.
func (p *Pool) QueueDepth() int {
	return len(p.fast) + len(p.heavy)
}

// WorkersBusy returns the number of workers currently processing.
func (p *Pool) WorkersBusy() int {
	return int(p.busy.Load())
}

// Close stops the workers and waits for them to finish.
func (p *Pool) Close() {
	close(p.done)
	p.wg.Wait()
}