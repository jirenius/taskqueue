package taskqueue

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const testTimeout = time.Second

func receive(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(testTimeout):
		t.Fatalf("timed out waiting for %s", what)
	}
}

func mustNotReceive(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
		t.Fatalf("unexpectedly received %s", what)
	case <-time.After(25 * time.Millisecond):
	}
}

func TestTaskQueueFlushWaitsForActiveFinalTask(t *testing.T) {
	tq := NewTaskQueue(1)
	started := make(chan struct{})
	release := make(chan struct{})
	flushed := make(chan struct{})
	tq.Do(func() {
		close(started)
		<-release
	})
	receive(t, started, "task start")
	go func() { tq.Flush(); close(flushed) }()
	mustNotReceive(t, flushed, "Flush before the final task finishes")
	close(release)
	receive(t, flushed, "Flush after the final task finishes")
}

func TestTaskQueueFlushWaitsForPendingAndRunningWork(t *testing.T) {
	tq := NewTaskQueue(2)
	started := make(chan struct{})
	release := make(chan struct{})
	second := make(chan struct{})
	flushed := make(chan struct{})
	tq.Do(func() { close(started); <-release })
	tq.Do(func() { close(second) })
	receive(t, started, "first task start")
	go func() { tq.Flush(); close(flushed) }()
	mustNotReceive(t, flushed, "Flush while work is pending")
	close(release)
	receive(t, second, "pending task")
	receive(t, flushed, "Flush after all work")
}

func TestTaskQueuePauseBeforeProcessingBegins(t *testing.T) {
	tq := NewTaskQueue(2)
	tq.Pause()
	ran := make(chan struct{})
	tq.Do(func() { close(ran) })
	mustNotReceive(t, ran, "task while paused")
	tq.Resume()
	receive(t, ran, "task after resume")
}

func TestTaskQueuePauseWaitsForRunningTask(t *testing.T) {
	tq := NewTaskQueue(2)
	started := make(chan struct{})
	release := make(chan struct{})
	paused := make(chan struct{})
	second := make(chan struct{})
	tq.Do(func() { close(started); <-release })
	tq.Do(func() { close(second) })
	receive(t, started, "first task start")
	go func() { tq.Pause(); close(paused) }()
	mustNotReceive(t, paused, "Pause while task is active")
	close(release)
	receive(t, paused, "Pause after task completion")
	mustNotReceive(t, second, "second task while paused")
	tq.Resume()
	receive(t, second, "second task after resume")
}

func TestTaskQueueNestedPauseRequiresMatchingResumes(t *testing.T) {
	tq := NewTaskQueue(1)
	tq.Pause()
	tq.Pause()
	ran := make(chan struct{})
	tq.Do(func() { close(ran) })
	tq.Resume()
	mustNotReceive(t, ran, "task after only one matching Resume")
	tq.Resume()
	receive(t, ran, "task after final Resume")
}

func TestTaskQueueBlockedProducerUnblocksWhenWorkerFreesCapacity(t *testing.T) {
	tq := NewTaskQueue(1)
	tq.Pause()
	started := make(chan struct{})
	release := make(chan struct{})
	producerDone := make(chan struct{})
	tq.Do(func() { close(started); <-release })
	go func() { tq.Do(func() {}); close(producerDone) }()
	mustNotReceive(t, producerDone, "producer while queue is full")
	tq.Resume()
	receive(t, started, "first task start")
	receive(t, producerDone, "producer after worker freed capacity")
	close(release)
	tq.Flush()
}

func TestTaskQueueConcurrentProducersAtFullCapacity(t *testing.T) {
	const producers = 12
	tq := NewTaskQueue(1)
	tq.Pause()
	tq.Do(func() {})

	var submitted sync.WaitGroup
	var executed sync.WaitGroup
	submitted.Add(producers)
	executed.Add(producers)
	for i := 0; i < producers; i++ {
		go func() {
			tq.Do(func() { executed.Done() })
			submitted.Done()
		}()
	}

	blocked := make(chan struct{})
	go func() { submitted.Wait(); close(blocked) }()
	mustNotReceive(t, blocked, "concurrent producers while queue is full and paused")
	tq.Resume()
	receive(t, blocked, "all concurrent producers")
	waitGroup(t, &executed, "concurrent producer tasks")
	tq.Flush()
}

func TestTaskQueueRestartsAfterBecomingIdle(t *testing.T) {
	tq := NewTaskQueue(1)
	first := make(chan struct{})
	second := make(chan struct{})
	tq.Do(func() { close(first) })
	tq.Flush()
	receive(t, first, "first task")
	tq.Do(func() { close(second) })
	tq.Flush()
	receive(t, second, "task after worker restart")
}

func TestTaskQueuePanicDoesNotStrandLaterTasks(t *testing.T) {
	tq := NewTaskQueue(2)
	ran := make(chan struct{})
	tq.Do(func() { panic("task failure") })
	tq.Do(func() { close(ran) })
	tq.Flush()
	receive(t, ran, "task after panic")
}

func TestTaskQueueOnPanicReceivesRecoveredPanic(t *testing.T) {
	panics := make(chan interface{}, 1)
	tq := NewTaskQueueWithOptions(2, TaskQueueOptions{
		OnPanic: func(panicValue interface{}) { panics <- panicValue },
	})
	ran := make(chan struct{})
	tq.Do(func() { panic("task failure") })
	tq.Do(func() { close(ran) })
	tq.Flush()
	receive(t, ran, "task after panic")
	select {
	case got := <-panics:
		if got != "task failure" {
			t.Fatalf("OnPanic received %#v, want %#v", got, "task failure")
		}
	case <-time.After(testTimeout):
		t.Fatal("timed out waiting for OnPanic")
	}
}

func TestTaskQueueOnPanicHandlerCannotStopQueue(t *testing.T) {
	handlerStarted := make(chan struct{})
	tq := NewTaskQueueWithOptions(2, TaskQueueOptions{
		OnPanic: func(interface{}) {
			close(handlerStarted)
			panic("handler failure")
		},
	})
	ran := make(chan struct{})
	tq.Do(func() { panic("task failure") })
	tq.Do(func() { close(ran) })
	tq.Flush()
	receive(t, ran, "task after a panicking OnPanic handler")
	receive(t, handlerStarted, "panicking OnPanic handler")
}

func TestTaskQueueReentrantPauseAndFlushUseNonBlockingVariants(t *testing.T) {
	tq := NewTaskQueue(2)
	requested := make(chan struct{})
	second := make(chan struct{})
	var flushed <-chan struct{}
	tq.Do(func() {
		tq.RequestPause()
		flushed = tq.FlushAsync()
		close(requested)
	})
	tq.Do(func() { close(second) })
	receive(t, requested, "task's pause and asynchronous flush request")
	mustNotReceive(t, flushed, "asynchronous Flush while paused work is pending")
	mustNotReceive(t, second, "task while paused by the running task")
	tq.Resume()
	receive(t, second, "pending task after resume")
	receive(t, flushed, "asynchronous Flush after all work")
}

func TestKeyTaskQueuePanicDoesNotStrandLaterTasks(t *testing.T) {
	tq := NewKeyTaskQueue(2)
	ran := make(chan struct{})
	tq.Do("key", func() { panic("task failure") })
	tq.Do("key", func() { close(ran) })
	receive(t, ran, "keyed task after panic")
}

func TestTaskQueueTryDoIsSafeFromRunningTask(t *testing.T) {
	tq := NewTaskQueue(1)
	var ran int32
	tq.Do(func() {
		if !tq.TryDo(func() { atomic.AddInt32(&ran, 1) }) {
			t.Error("TryDo from a running task unexpectedly found queue full")
		}
	})
	tq.Flush()
	if got := atomic.LoadInt32(&ran); got != 1 {
		t.Fatalf("nested task ran %d times, want 1", got)
	}
}

func waitGroup(t *testing.T, wg *sync.WaitGroup, what string) {
	t.Helper()
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	receive(t, done, what)
}
