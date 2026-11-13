package taskqueue

import "sync"

// KeyTaskQueue implements a queue for tasks to be handled sequentially in
// a first-in-first-out order on separate goroutines identified by a key string.
// Panics from tasks are recovered so that later work for the same key continues.
// A task may call TryDo, but must not call Do if its key's waiting queue is
// full, because it would block the only worker that can free capacity.
type KeyTaskQueue struct {
	mu      sync.Mutex
	qpool   sync.Pool
	qs      map[string]*taskQueue
	cap     int
	onPanic func(interface{})
}

// KeyTaskQueueOptions configures a KeyTaskQueue.
type KeyTaskQueueOptions struct {
	// OnPanic is called asynchronously for each recovered task panic. A panic
	// in the handler is recovered and ignored so it cannot stop the queue.
	OnPanic func(interface{})
}

type taskQueue struct {
	c      sync.Cond
	q      []func()
	offset int
	size   int
}

// NewKeyTaskQueue returns a new KeyTaskQueue.
//
// The value cap is the number of tasks that may be queue at the same time,
// and must be 1 or greater.
func NewKeyTaskQueue(cap int) *KeyTaskQueue {
	return NewKeyTaskQueueWithOptions(cap, KeyTaskQueueOptions{})
}

// NewKeyTaskQueueWithOptions returns a new KeyTaskQueue configured with options.
func NewKeyTaskQueueWithOptions(cap int, options KeyTaskQueueOptions) *KeyTaskQueue {
	if cap < 1 {
		panic("cap must be 1 or greater")
	}
	nq := &KeyTaskQueue{
		qs:      make(map[string]*taskQueue),
		cap:     cap,
		onPanic: options.OnPanic,
	}
	nq.qpool.New = nq.newQueue
	return nq
}

// Do will enqueue the task to be handled by the task queue for the given key.
// If the queue is full, Do will wait block until there is room in the queue.
func (nq *KeyTaskQueue) Do(key string, task func()) {
	nq.mu.Lock()
	var q *taskQueue
	var ok bool
	for {
		q, ok = nq.qs[key]
		if ok {
			// If a full queue exists, wait for room
			if q.size == nq.cap {
				q.c.Wait()
				continue
			}
		} else {
			q = nq.qpool.Get().(*taskQueue)
			nq.qs[key] = q
			go nq.processQueue(key, q)
		}
		break
	}
	nq.addTask(q, task)
	nq.mu.Unlock()
}

// TryDo will try to enqueue the task and return true on success.
// It will return false if the work queue for the given key is full.
func (nq *KeyTaskQueue) TryDo(key string, task func()) bool {
	nq.mu.Lock()
	defer nq.mu.Unlock()
	q, ok := nq.qs[key]
	if ok {
		// If a full queue exists
		if q.size == nq.cap {
			return false
		}
	} else {
		q = nq.qpool.Get().(*taskQueue)
		nq.qs[key] = q
		go nq.processQueue(key, q)
	}
	nq.addTask(q, task)
	return true
}

func (nq *KeyTaskQueue) newQueue() interface{} {
	return &taskQueue{
		q: make([]func(), nq.cap),
		c: sync.Cond{L: &nq.mu},
	}
}

func (nq *KeyTaskQueue) addTask(q *taskQueue, task func()) {
	// Add task to queue
	q.q[(q.offset+q.size)%nq.cap] = task
	q.size++
}

func (nq *KeyTaskQueue) processQueue(key string, q *taskQueue) {
	defer nq.workerStopped(key, q)
	for {
		nq.mu.Lock()
		if q.size == 0 {
			nq.mu.Unlock()
			return
		}
		// Get next task
		task := q.q[q.offset]
		q.q[q.offset] = nil // Prevent memory leaks
		q.offset = (q.offset + 1) % nq.cap
		// If queue was full, signal that one slot is now free.
		if q.size == nq.cap {
			q.c.Signal()
		}
		q.size--
		nq.mu.Unlock()
		// Perform task
		nq.invokeTask(task)
	}
}

// invokeTask keeps a task panic from stranding later work for the same key.
func (nq *KeyTaskQueue) invokeTask(task func()) {
	defer func() {
		if panicValue := recover(); panicValue != nil && nq.onPanic != nil {
			go callPanicHandler(nq.onPanic, panicValue)
		}
	}()
	task()
}

func (nq *KeyTaskQueue) workerStopped(key string, q *taskQueue) {
	nq.mu.Lock()
	defer nq.mu.Unlock()
	if q.size > 0 {
		// A producer can enqueue while the worker is shutting down; runtime.Goexit
		// in a task reaches this path too. Continue the key's work in either case.
		go nq.processQueue(key, q)
		return
	}
	delete(nq.qs, key)
	nq.qpool.Put(q)
}
