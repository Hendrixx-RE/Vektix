package queue

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Task represents an asynchronous background job.
type Task struct {
	ID        string
	Type      string
	Payload   []byte
	Attempt   int
	MaxRetry  int
	CreatedAt time.Time
}

// DeadLetterQueue stores jobs that exceeded their retry threshold.
type DeadLetterQueue struct {
	failedTasks []Task
	mu          sync.Mutex
}

// Push records a failed task to the dead letter queue.
func (d *DeadLetterQueue) Push(task Task) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.failedTasks = append(d.failedTasks, task)
}

// WorkerPool manages concurrent worker goroutines processing job queues.
type WorkerPool struct {
	concurrency int
	taskChan    chan Task
	dlq         *DeadLetterQueue
	wg          sync.WaitGroup
}

// NewWorkerPool creates a worker pool with bounded channel capacity.
func NewWorkerPool(concurrency int, bufferSize int) *WorkerPool {
	return &WorkerPool{
		concurrency: concurrency,
		taskChan:    make(chan Task, bufferSize),
		dlq:         &DeadLetterQueue{},
	}
}

// StartWorkers launches the worker goroutines.
func (p *WorkerPool) StartWorkers(ctx context.Context, handler func(Task) error) {
	for i := 0; i < p.concurrency; i++ {
		p.wg.Add(1)
		go func(workerID int) {
			defer p.wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case task, ok := <-p.taskChan:
					if !ok {
						return
					}
					if err := handler(task); err != nil {
						task.Attempt++
						if task.Attempt > task.MaxRetry {
							p.dlq.Push(task)
						} else {
							p.taskChan <- task
						}
					}
				}
			}
		}(i)
	}
}

// Enqueue submits a new task to the worker pool channel.
func (p *WorkerPool) Enqueue(task Task) error {
	select {
	case p.taskChan <- task:
		return nil
	default:
		return fmt.Errorf("queue capacity reached")
	}
}
