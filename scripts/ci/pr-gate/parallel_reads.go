package main

import (
	"context"
	"errors"
	"sync"
)

// Read only two independent inputs within one authority phase. The caller
// validates both after joining; final authority phases still perform fresh
// reads. Cancellation and either error must finish both readers before return.
func parallelReads(ctx context.Context, first, second func(context.Context) error) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var wait sync.WaitGroup
	var failures [2]error
	for i, read := range [2]func(context.Context) error{first, second} {
		wait.Add(1)
		go func(index int, reader func(context.Context) error) {
			defer wait.Done()
			failures[index] = reader(ctx)
			if failures[index] != nil {
				cancel()
			}
		}(i, read)
	}
	wait.Wait()
	return errors.Join(failures[:]...)
}

// settle runs independent reads at once and returns when all have finished.
// Unlike parallelReads it never cancels the others after one fails: the caller
// keeps each result and decides in the order the serial code would have.
func settle(ctx context.Context, reads ...func(context.Context)) {
	var wait sync.WaitGroup
	for _, read := range reads {
		wait.Add(1)
		go func() {
			defer wait.Done()
			read(ctx)
		}()
	}
	wait.Wait()
}
