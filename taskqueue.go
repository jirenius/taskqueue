package taskqueue

import "sync"

// TaskQueue implements a queue for tasks to be handled sequentially in
// a first-in-first-out order on a separate goroutine.
type TaskQueue struct {
	c       *sync.Cond
	q       []func()
	cap     int
	working bool
	intask  bool
	offset  int
	size    int
	flush   *sync.Cond
	pauses  int
	pause   *sync.Cond
}

// NewTaskQueue returns a new TaskQueue.
//
// The value cap is the number of tasks that may be queue at the same time,
// and must be 1 or greater.
func NewTaskQueue(cap int) *TaskQueue {
	if cap < 1 {
		panic("cap must be 1 or greater")
	}
	l := sync.Mutex{}
	return &TaskQueue{
		c:     sync.NewCond(&l),
		q:     make([]func(), cap),
		cap:   cap,
		flush: sync.NewCond(&l),
		pause: sync.NewCond(&l),
	}
}

// Do will enqueue the task to be handled by the task queue.
// If the queue is full, Do will wait block until there is room in the queue.
func (tq *TaskQueue) Do(task func()) {
	tq.c.L.Lock()
	// Wait until there is room in the queue
	for tq.size == tq.cap {
		tq.c.Wait()
	}
	tq.addTask(task)
	tq.c.L.Unlock()
}

// TryDo will try to enqueue the task and return true on success.
// It will return false if the work queue is full.
func (tq *TaskQueue) TryDo(task func()) bool {
	tq.c.L.Lock()
	defer tq.c.L.Unlock()
	// Check if there is room in the queue
	for tq.size == tq.cap {
		return false
	}
	tq.addTask(task)
	return true
}

// Flush waits for the queue to be cleared.
func (tq *TaskQueue) Flush() {
	tq.flush.L.Lock()
	// Wait until the queue is empty
	for tq.size > 0 {
		tq.flush.Wait()
	}
	tq.flush.L.Unlock()
}

// Pause stops handling new tasks, and waits until any currently running task is complete. If Pause is called multiple times, an equal
// number of calls to Resume must be made to resume handling of tasks.
func (tq *TaskQueue) Pause() {
	tq.pause.L.Lock()
	defer tq.pause.L.Unlock()
	tq.pauses++
	// If a task is being handled, wait until it is done, or Resume is called.
	for tq.intask {
		tq.pause.Wait()
	}
}

// Resume starts task handling again after being paused by calling Pause.
func (tq *TaskQueue) Resume() {
	tq.pause.L.Lock()
	defer tq.pause.L.Unlock()
	if tq.pauses == 0 {
		panic("taskqueue is not paused")
	}
	tq.pauses--
	if tq.pauses == 0 {
		tq.pause.Broadcast()
	}
}

func (tq *TaskQueue) addTask(task func()) {
	// If we don't have a worker goroutine, we start one.
	if !tq.working {
		tq.working = true
		go tq.processQueue()
	}
	// Add task to queue
	tq.q[(tq.offset+tq.size)%tq.cap] = task
	tq.size++
}

func (tq *TaskQueue) processQueue() {
	tq.c.L.Lock()
	for tq.size > 0 {
		// Get next task
		task := tq.q[tq.offset]
		tq.q[tq.offset] = nil // Prevent memory leaks
		tq.offset = (tq.offset + 1) % tq.cap
		// If queue was full, signal that one slot is now free.
		if tq.size == tq.cap {
			tq.c.Signal()
		}
		tq.size--
		// Check if paused
		for tq.pauses > 0 {
			tq.pause.Wait()
		}
		tq.intask = true
		tq.c.L.Unlock()
		// Perform task
		task()
		tq.c.L.Lock()
		tq.intask = false
		// Broadcast to all calls to Pause to continue
		tq.pause.Broadcast()
	}
	tq.working = false
	tq.flush.Broadcast()
	tq.c.L.Unlock()
}
