// Package workerpool provides a bounded concurrent worker pool.
//
// Workers process tasks from a shared channel. The pool manages
// lifecycle (start/stop), error collection, and graceful shutdown.
package workerpool

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
)

// Task represents a unit of work to be processed by a worker.
type Task interface {
	// ID returns a unique identifier for this task.
	ID() string
	// Execute runs the task. It should respect context cancellation.
	Execute(ctx context.Context) error
}

// Result captures the outcome of a single task execution.
type Result struct {
	TaskID string
	Err    error
}

// Pool manages a fixed number of workers processing tasks concurrently.
type Pool struct {
	size       int
	tasks      chan Task
	results    chan Result
	wg         sync.WaitGroup
	processed  atomic.Int64
	failed     atomic.Int64
	cancelFunc context.CancelFunc
}

// New creates a worker pool with the given number of workers
// and task buffer size.
func New(workerCount, bufferSize int) *Pool {
	if workerCount < 1 {
		workerCount = 1
	}
	return &Pool{
		size:    workerCount,
		tasks:   make(chan Task, bufferSize),
		results: make(chan Result, bufferSize),
	}
}

// Start launches all workers and begins processing tasks.
// Returns a cancel function to stop the pool.
func (p *Pool) Start(ctx context.Context) context.CancelFunc {
	ctx, cancel := context.WithCancel(ctx)
	p.cancelFunc = cancel

	for i := 0; i < p.size; i++ {
		p.wg.Add(1)
		go p.worker(ctx, i)
	}

	return cancel
}

// Submit adds a task to the pool. Blocks if the buffer is full.
// Returns an error if the pool is shut down.
func (p *Pool) Submit(task Task) error {
	select {
	case p.tasks <- task:
		return nil
	default:
		return errors.New("worker pool buffer full")
	}
}

// Results returns the channel of task results.
func (p *Pool) Results() <-chan Result {
	return p.results
}

// Wait blocks until all workers have finished and closes the results channel.
func (p *Pool) Wait() {
	close(p.tasks)
	p.wg.Wait()
	close(p.results)
}

// Stats returns pool execution statistics.
func (p *Pool) Stats() (processed, failed int64) {
	return p.processed.Load(), p.failed.Load()
}

func (p *Pool) worker(ctx context.Context, id int) {
	defer p.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case task, ok := <-p.tasks:
			if !ok {
				return
			}
			err := p.executeTask(ctx, task)
			p.results <- Result{TaskID: task.ID(), Err: err}
		}
	}
}

func (p *Pool) executeTask(ctx context.Context, task Task) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic in task %s: %v", task.ID(), r)
		}
		p.processed.Add(1)
		if err != nil {
			p.failed.Add(1)
		}
	}()
	return task.Execute(ctx)
}
