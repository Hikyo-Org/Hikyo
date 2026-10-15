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
		go func() {
			defer wait.Done()
			failures[i] = read(ctx)
			if failures[i] != nil {
				cancel()
			}
		}()
	}
	wait.Wait()
	return errors.Join(failures[:]...)
}
