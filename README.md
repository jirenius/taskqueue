# Task Queues for Go

[![License MIT](https://img.shields.io/badge/License-MIT-blue.svg)](http://opensource.org/licenses/MIT)
[![Report Card](http://goreportcard.com/badge/github.com/jirenius/taskqueue)](http://goreportcard.com/report/jirenius/taskqueue)
[![Build Status](https://travis-ci.com/jirenius/taskqueue.svg?branch=master)](https://travis-ci.com/jirenius/taskqueue)
[![Reference](https://img.shields.io/static/v1?label=reference&message=go.dev&color=5673ae)](https://pkg.go.dev/github.com/jirenius/taskqueue)

A [Go](http://golang.org) package that provides queues for tasks to be handled sequentially.

## Installation

```bash
go get github.com/jirenius/taskqueue
```

## Usage

### TaskQueue
```go
    // Create a TaskQueue with a queue cap of 5.
    tq := taskqueue.NewTaskQueue(5)

    // Add callbacks to be called sequentially in the queued order.
    tq.Do(func() { fmt.Println("First") })
    tq.Do(func() { fmt.Println("Second") })

    // Add a callback unless the queue is full.
    if !tq.TryDo(func() { fmt.Println("Done") }) {
        panic("queue is full")
    }
```

`Flush` is a completion barrier: when it returns, no task is waiting and no
task is running. `Pause` prevents additional tasks from starting and waits for
an already-running task to finish; pauses are nested and require matching calls
to `Resume`.

Task panics are recovered so later queued tasks continue to execute. A callback
may use `TryDo`, and may use `Do` only when it knows the waiting queue has room.
Callbacks must not call `Pause` or `Flush`: either call waits for the callback
itself to return and would therefore deadlock. Callbacks can instead use
`RequestPause` and `FlushAsync` (without waiting for the returned channel before
the callback returns).

To observe recovered panics, construct the queue with `TaskQueueOptions` and
an `OnPanic` callback. The callback runs asynchronously; its own panic is
recovered and ignored.

### KeyTaskQueue
```go
    // Create a KeyTaskQueue with a queue cap of 5 per key.
    ktq := taskqueue.NewKeyTaskQueue(5)

    // Add callbacks to be called sequentially, on different
    // goroutines for each key, in the queued order.
    ktq.Do("foo", func() { fmt.Println("First foo") })
    ktq.Do("foo", func() { fmt.Println("Second foo") })
    ktq.Do("bar", func() { fmt.Println("First bar") })

    // Add a callback unless the queue is full.
    if !ktq.TryDo("foo", func() { fmt.Println("Done") }) {
        panic("foo queue is full")
    }
```

Keyed queues also recover task panics and continue the remaining work for that
key. As with `TaskQueue`, a callback should use `TryDo` rather than call `Do`
when its key's waiting queue might be full. Use `KeyTaskQueueOptions.OnPanic`
with `NewKeyTaskQueueWithOptions` to observe recovered panics.
