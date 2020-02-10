package taskqueue_test

import (
	"fmt"

	"github.com/jirenius/taskqueue"
)

func Example() {
	ch := make(chan string)
	// Create TaskQueue with a queue cap of 5.
	tq := taskqueue.NewTaskQueue(5)
	// The callback functions are called sequentially on
	// a different goroutine, in the queued order.
	tq.Do(func() { ch <- "First" })
	tq.Do(func() { ch <- "Second" })
	tq.Do(func() { close(ch) })
	// Print strings sent from queued callbacks.
	for s := range ch {
		fmt.Println(s)
	}

	// Output:
	// First
	// Second
}
