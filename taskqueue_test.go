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

func TestTaskQueueFlush_Single_FlushesQueue(t *testing.T) {
	v := validator{t: t}
	tq := NewTaskQueue(4)
	v.Add(3)
	tq.Do(func() { v.Done("a") })
	tq.Do(func() { v.Done("b") })
	tq.Do(func() { v.Done("c") })
	tq.Flush()
	v.Is("abc")
	v.WaitTimeout()
}

func TestTaskQueueFlush_Multiple_FlushesQueue(t *testing.T) {
	v := validator{t: t}
	tq := NewTaskQueue(3)
	v.Add(3 + 5)
	tq.Do(func() { v.Done("a") })
	tq.Do(func() { v.Done("b") })
	tq.Do(func() { v.Done("c") })
	for i := 0; i < 5; i++ {
		go func() {
			tq.Flush()
			v.Is("abc")
			v.Done("")
		}()
	}
	v.Validate("abc")
}

func TestTaskQueuePause_OnEmpty_WaitsUntilResume(t *testing.T) {
	v := validator{t: t}
	tq := NewTaskQueue(5)
	tq.Pause()
	v.Add(3)
	tq.Do(func() { v.Done("a") })
	tq.Do(func() { v.Done("b") })
	tq.Do(func() { v.Done("c") })
	v.ValidateNotDone("")
	tq.Resume()
	v.Validate("abc")
}

func TestTaskQueuePause_CalledTwice_WaitsUntilResumeCalledTwice(t *testing.T) {
	v := validator{t: t}
	tq := NewTaskQueue(5)
	tq.Pause()
	tq.Pause()
	v.Add(3)
	tq.Do(func() { v.Done("a") })
	tq.Do(func() { v.Done("b") })
	tq.Do(func() { v.Done("c") })
	tq.Resume()
	v.ValidateNotDone("")
	tq.Resume()
	v.Validate("abc")
}

func TestTaskQueuePause_DuringTask_WaitsUntilResume(t *testing.T) {
	v := validator{t: t}
	tq := NewTaskQueue(5)
	v.Add(3)
	tq.Do(func() {
		v.Done("a")
		pause := make(chan bool)
		go func() {
			go func() { pause <- true }()
			tq.Pause()
			v.Done("b")
		}()
		<-pause
	})
	tq.Do(func() { v.Done("c") })
	v.ValidateNotDone("ab")
	tq.Resume()
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

// WaitTimeout waits for the waitgroup for the specified max timeout.
// Returns true if waiting timed out.
func (v *validator) WaitTimeout() {
	c := make(chan struct{})
	go func() {
		defer close(c)
		v.Wait()
	}()
	select {
	case <-c:
	case <-time.After(time.Second):
		v.t.Fatalf("Timed out")
	}
}

func (v *validator) Validate(expected string) {
	v.WaitTimeout()
	if expected != v.s {
		v.t.Errorf("Expected %#v, but got %#v", expected, v.s)
	}
}

func (v *validator) ValidateNotDone(expected string) {
	c := make(chan struct{})
	go func() {
		defer close(c)
		v.Wait()
	}()
	select {
	case <-c:
		v.t.Fatalf("Is unexpectedly done")
	case <-time.After(time.Millisecond):
		if expected != v.s {
			v.t.Errorf("Expected %#v, but got %#v", expected, v.s)
		}
	}
}

func (v *validator) Is(expected string) {
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
