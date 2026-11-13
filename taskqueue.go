package taskqueue

import "sync"

// TaskQueue implements a queue for tasks to be handled sequentially in
// first-in-first-out order on a separate goroutine.
//
// A task may call TryDo. It may call Do only when it can establish that space
// is available; Do waiting for a full queue would prevent this single worker
// from freeing space. A task must not call Pause or Flush: both operations wait
// for the currently running task to return, so calling either from that task
// would wait for itself forever. Go does not provide a safe goroutine identity
// with which to distinguish those cases from ordinary concurrent callers. A
// task can use RequestPause and FlushAsync instead.
//
// Panics from tasks are recovered and discarded so that later queued tasks
// continue to run. This also applies to a nil task, which panics when invoked.
type TaskQueue struct {
	mu sync.Mutex

	// notFull is waited on only while size == cap. Its predicate is protected
	// by mu. state is waited on while a pause or flush predicate is false; its
	// predicates (size, active, and pauses) are also protected by mu.
	notFull *sync.Cond
	state   *sync.Cond

	q      []func()
	head   int
	size   int
	cap    int
	worker bool
	active bool
	pauses int
}

// NewTaskQueue returns a new TaskQueue.
//
// The value cap is the number of waiting tasks that may be queued at the same
// time, and must be 1 or greater.
func NewTaskQueue(cap int) *TaskQueue {
	if cap < 1 {
		panic("cap must be 1 or greater")
	}
	tq := &TaskQueue{
		q:   make([]func(), cap),
		cap: cap,
	}
	tq.notFull = sync.NewCond(&tq.mu)
	tq.state = sync.NewCond(&tq.mu)
	return tq
}

// Do enqueues task to be handled by the task queue. If the queue is full, Do
// blocks until there is room in the waiting queue.
func (tq *TaskQueue) Do(task func()) {
	tq.mu.Lock()
	defer tq.mu.Unlock()
	for tq.size == tq.cap {
		tq.notFull.Wait()
	}
	tq.addTaskLocked(task)
}

// TryDo tries to enqueue task and returns true on success. It returns false if
// the waiting queue is full.
func (tq *TaskQueue) TryDo(task func()) bool {
	tq.mu.Lock()
	defer tq.mu.Unlock()
	if tq.size == tq.cap {
		return false
	}
	tq.addTaskLocked(task)
	return true
}

// Flush waits until there are no waiting tasks and no task is executing. Work
// submitted after Flush has observed that state is not covered by the barrier.
// Flush must not be called by a task running on this queue; see TaskQueue.
func (tq *TaskQueue) Flush() {
	tq.mu.Lock()
	defer tq.mu.Unlock()
	for tq.size != 0 || tq.active {
		tq.state.Wait()
	}
}

// Pause prevents new tasks from starting and waits for a currently executing
// task to finish. Calls nest: each successful Pause requires one Resume before
// processing continues. Pause must not be called by a task running on this
// queue; see TaskQueue.
func (tq *TaskQueue) Pause() {
	tq.mu.Lock()
	defer tq.mu.Unlock()
	tq.pauses++
	for tq.active {
		tq.state.Wait()
	}
}

// RequestPause prevents new tasks from starting without waiting for a task that
// is already executing. It otherwise nests exactly like Pause, and each call
// requires one Resume. It is safe to call from a task running on this queue.
func (tq *TaskQueue) RequestPause() {
	tq.mu.Lock()
	tq.pauses++
	tq.mu.Unlock()
}

// Resume starts task handling again after matching calls to Pause or
// RequestPause.
func (tq *TaskQueue) Resume() {
	tq.mu.Lock()
	defer tq.mu.Unlock()
	if tq.pauses == 0 {
		panic("taskqueue is not paused")
	}
	tq.pauses--
	if tq.pauses == 0 {
		tq.state.Broadcast()
	}
}

// FlushAsync returns a channel that is closed when Flush's completion condition
// is met. It is safe to call from a task running on this queue; that task must
// not wait for the returned channel before returning.
func (tq *TaskQueue) FlushAsync() <-chan struct{} {
	done := make(chan struct{})
	go func() {
		tq.Flush()
		close(done)
	}()
	return done
}

// addTaskLocked adds a task and starts a worker if needed. mu must be held.
func (tq *TaskQueue) addTaskLocked(task func()) {
	tq.q[(tq.head+tq.size)%tq.cap] = task
	tq.size++
	if !tq.worker {
		tq.worker = true
		go tq.processQueue()
	}
}

func (tq *TaskQueue) processQueue() {
	// This defer also covers runtime.Goexit in a task. invokeTask clears active
	// first; if work remains, starting a replacement worker keeps it live.
	defer tq.workerStopped()

	for {
		tq.mu.Lock()
		for tq.pauses > 0 && tq.size > 0 {
			tq.state.Wait()
		}
		if tq.size == 0 {
			tq.mu.Unlock()
			return
		}

		task := tq.q[tq.head]
		tq.q[tq.head] = nil // Do not retain completed tasks.
		tq.head = (tq.head + 1) % tq.cap
		tq.size--
		tq.active = true
		tq.notFull.Signal()
		tq.mu.Unlock()

		tq.invokeTask(task)
	}
}

func (tq *TaskQueue) invokeTask(task func()) {
	defer func() {
		// A task panic must not terminate the worker or leave active set. Goexit
		// has a nil recover value, but this deferred cleanup still runs.
		_ = recover()
		tq.mu.Lock()
		tq.active = false
		tq.state.Broadcast()
		tq.mu.Unlock()
	}()
	task()
}

func (tq *TaskQueue) workerStopped() {
	tq.mu.Lock()
	defer tq.mu.Unlock()
	tq.worker = false
	if tq.size > 0 {
		// A producer can enqueue between the worker's empty check and this
		// deferred cleanup; runtime.Goexit in a task reaches this path too.
		// Keep the queue live in both cases.
		tq.worker = true
		go tq.processQueue()
	}
	tq.state.Broadcast()
}
