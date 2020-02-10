package taskqueue

import (
	"sync"
	"testing"
	"time"
)

func TestTaskQueueDo_Single_Called(t *testing.T) {
	v := validator{t: t}
	tq := NewTaskQueue(5)
	v.Add(1)
	tq.Do(func() { v.Done("a") })
	v.Validate("a")
}

func TestTaskQueueDo_Serial_CalledInOrder(t *testing.T) {
	v := validator{t: t}
	tq := NewTaskQueue(5)
	v.Add(3)
	tq.Do(func() { v.Done("a") })
	tq.Do(func() { v.Done("b") })
	tq.Do(func() { v.Done("c") })
	v.Validate("abc")
}

func TestTaskQueueTryDo_Single_Called(t *testing.T) {
	v := validator{t: t}
	tq := NewTaskQueue(5)
	v.Add(1)
	v.IsTrue(tq.TryDo(func() { v.Done("a") }))
	v.Validate("a")
}

func TestTaskQueueTryDo_Serial_CalledInOrder(t *testing.T) {
	v := validator{t: t}
	tq := NewTaskQueue(5)
	v.Add(3)
	v.IsTrue(tq.TryDo(func() { v.Done("a") }))
	v.IsTrue(tq.TryDo(func() { v.Done("b") }))
	v.IsTrue(tq.TryDo(func() { v.Done("c") }))
	v.Validate("abc")
}

// validator embeds a waitgroup, and validates that the order
// Done is called is correct.
type validator struct {
	sync.WaitGroup
	t *testing.T
	s string
}

func (v *validator) Done(a string) {
	v.s += a
	v.WaitGroup.Done()
}

// waitTimeout waits for the waitgroup for the specified max timeout.
// Returns true if waiting timed out.
func waitTimeout(wg *sync.WaitGroup, timeout time.Duration) bool {
	c := make(chan struct{})
	go func() {
		defer close(c)
		wg.Wait()
	}()
	select {
	case <-c:
		return false // completed normally
	case <-time.After(timeout):
		return true // timed out
	}
}

func (v *validator) Validate(expected string) {
	if waitTimeout(&v.WaitGroup, time.Second) {
		v.t.Errorf("Timed out")
		return
	}
	if expected != v.s {
		v.t.Errorf("Expected %#v, but got %#v", expected, v.s)
	}
}

func (v *validator) IsTrue(b bool) {
	if !b {
		v.t.Errorf("Expected true")
	}
}

func (v *validator) IsFalse(b bool) {
	if b {
		v.t.Errorf("Expected false")
	}
}
