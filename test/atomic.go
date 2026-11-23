package test

import (
	"errors"
	"fmt"
	"math/rand"
	"strconv"
	"sync"
	"testing"

	"github.com/philippgille/gokv"
)

// TestAtomicOperations verifies the optional atomic update interfaces:
// no lost updates under concurrent increments, conflict detection, and
// aborting an update without writing when the update function fails.
func TestAtomicOperations(t *testing.T, store gokv.Store) {
	updater, ok := store.(gokv.AtomicUpdater)
	if !ok {
		t.Fatal("store does not implement gokv.AtomicUpdater")
	}
	comparer, ok := store.(gokv.AtomicComparer)
	if !ok {
		t.Fatal("store does not implement gokv.AtomicComparer")
	}
	incrementer, ok := store.(gokv.AtomicIncrementer)
	if !ok {
		t.Fatal("store does not implement gokv.AtomicIncrementer")
	}

	t.Run("concurrent updates do not lose writes", func(t *testing.T) {
		key := "atomic-" + strconv.FormatInt(rand.Int63(), 10)
		const goroutines = 2
		const incrementsPerGoroutine = 10000

		errs := make(chan error, goroutines)
		var waitGroup sync.WaitGroup
		waitGroup.Add(goroutines)
		for i := 0; i < goroutines; i++ {
			go func() {
				defer waitGroup.Done()
				for j := 0; j < incrementsPerGoroutine; j++ {
					var value int
					err := updater.Update(key, &value, func(found bool) error {
						value++
						return nil
					})
					if err != nil {
						errs <- err
						return
					}
				}
			}()
		}
		waitGroup.Wait()
		close(errs)
		for err := range errs {
			t.Fatal(err)
		}

		var value int
		found, err := store.Get(key, &value)
		if err != nil {
			t.Fatal(err)
		}
		if !found {
			t.Fatal("counter was not stored")
		}
		expected := goroutines * incrementsPerGoroutine
		if value != expected {
			t.Fatalf("expected counter %d, got %d", expected, value)
		}
	})

	t.Run("update error aborts the write", func(t *testing.T) {
		key := "atomic-error-" + strconv.FormatInt(rand.Int63(), 10)
		if err := store.Set(key, 1); err != nil {
			t.Fatal(err)
		}

		expectedErr := errors.New("stop")
		var value int
		err := updater.Update(key, &value, func(found bool) error {
			value = 2
			return expectedErr
		})
		if !errors.Is(err, expectedErr) {
			t.Fatalf("expected %v, got %v", expectedErr, err)
		}

		var actual int
		found, err := store.Get(key, &actual)
		if err != nil {
			t.Fatal(err)
		}
		if !found || actual != 1 {
			t.Fatalf("expected unchanged value 1, got found=%v value=%d", found, actual)
		}
	})

	t.Run("compare-and-swap detects changes", func(t *testing.T) {
		key := "atomic-cas-" + strconv.FormatInt(rand.Int63(), 10)
		if err := store.Set(key, 10); err != nil {
			t.Fatal(err)
		}

		swapped, err := comparer.CompareAndSwap(key, 10, 11)
		if err != nil {
			t.Fatal(err)
		}
		if !swapped {
			t.Fatal("expected first compare-and-swap to succeed")
		}

		swapped, err = comparer.CompareAndSwap(key, 10, 12)
		if err != nil {
			t.Fatal(err)
		}
		if swapped {
			t.Fatal("stale compare-and-swap unexpectedly succeeded")
		}

		var value int
		found, err := store.Get(key, &value)
		if err != nil {
			t.Fatal(err)
		}
		if !found || value != 11 {
			t.Fatalf("expected value 11, got found=%v value=%d", found, value)
		}

		missingKey := "atomic-cas-missing-" + strconv.FormatInt(rand.Int63(), 10)
		swapped, err = comparer.CompareAndSwap(missingKey, nil, 1)
		if err != nil {
			t.Fatal(err)
		}
		if !swapped {
			t.Fatal("expected compare-and-swap to create a missing key")
		}
		swapped, err = comparer.CompareAndSwap(missingKey, nil, 2)
		if err != nil {
			t.Fatal(err)
		}
		if swapped {
			t.Fatal("compare-and-swap unexpectedly replaced an existing key")
		}
	})

	t.Run("increment treats a missing key as zero", func(t *testing.T) {
		key := "atomic-incr-" + strconv.FormatInt(rand.Int63(), 10)
		value, err := incrementer.Increment(key, 3)
		if err != nil {
			t.Fatal(err)
		}
		if value != 3 {
			t.Fatalf("expected 3, got %d", value)
		}
		value, err = incrementer.Increment(key, -2)
		if err != nil {
			t.Fatal(err)
		}
		if value != 1 {
			t.Fatalf("expected 1, got %d", value)
		}
	})
}

// RunConcurrentIncrements is the lost-update regression scenario: multiple
// goroutines each perform the requested number of increments on one counter.
func RunConcurrentIncrements(t *testing.T, store gokv.Store, key string, goroutines, increments int) {
	t.Helper()
	incrementer, ok := store.(gokv.AtomicIncrementer)
	if !ok {
		t.Fatal("store does not implement gokv.AtomicIncrementer")
	}

	errs := make(chan error, goroutines)
	var waitGroup sync.WaitGroup
	waitGroup.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer waitGroup.Done()
			for j := 0; j < increments; j++ {
				if _, err := incrementer.Increment(key, 1); err != nil {
					errs <- err
					return
				}
			}
		}()
	}
	waitGroup.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}

	var value int64
	found, err := store.Get(key, &value)
	if err != nil {
		t.Fatal(err)
	}
	expected := int64(goroutines * increments)
	if !found || value != expected {
		t.Fatal(fmt.Errorf("expected counter %d, got found=%v value=%d", expected, found, value))
	}
}
