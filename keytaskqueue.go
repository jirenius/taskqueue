package taskqueue

import "sync"

// KeyTaskQueue implements a queue for tasks to be handled sequentially in
// a first-in-first-out order on separate goroutines identified by a key string.
type KeyTaskQueue struct {
	mu    sync.Mutex
	qpool sync.Pool
	qs    map[string]*taskQueue
	cap   int
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
	if cap < 1 {
		panic("cap must be 1 or greater")
	}
	nq := &KeyTaskQueue{
		qs:  make(map[string]*taskQueue),
		cap: cap,
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
	nq.mu.Lock()
	for q.size > 0 {
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
		task()
		nq.mu.Lock()
	}
	delete(nq.qs, key)
	nq.qpool.Put(q)
	nq.mu.Unlock()
}
