package taskqueue_test

import (
	"fmt"

	"github.com/jirenius/taskqueue"
)

// ExampleKeyTaskQueue demonstrates how to enqueue callbacks on different
// goroutines based on key.
func Example_keyTaskQueue() {
	ach := make(chan string)
	bch := make(chan string)
	// Create KeyTaskQueue with a queue cap of 5 per key.
	tq := taskqueue.NewKeyTaskQueue(5)
	// The callback functions are called sequentially on
	// different goroutines for each key, in the queued order.
	tq.Do("a", func() { ach <- "First a" })
	tq.Do("b", func() { bch <- "First b" })
	tq.Do("a", func() { ach <- "Second a" })
	tq.Do("b", func() { bch <- "Second b" })
	tq.Do("a", func() { close(ach) })
	tq.Do("b", func() { close(bch) })
	// Print strings sent from queue a.
	for s := range ach {
		fmt.Println(s)
	}
	// Print strings sent from queue b.
	for s := range bch {
		fmt.Println(s)
	}

	// Output:
	// First a
	// Second a
	// First b
	// Second b
}
