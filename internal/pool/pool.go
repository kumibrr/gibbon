// Package pool runs a function over items with bounded parallelism.
package pool

import "sync"

// Map applies fn to every item using at most workers goroutines and returns
// results in input order.
func Map[T, R any](items []T, workers int, fn func(T) R) []R {
	out := make([]R, len(items))
	if len(items) == 0 {
		return out
	}
	if workers < 1 {
		workers = 1
	}
	if workers > len(items) {
		workers = len(items)
	}
	idx := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range idx {
				out[i] = fn(items[i])
			}
		}()
	}
	for i := range items {
		idx <- i
	}
	close(idx)
	wg.Wait()
	return out
}
