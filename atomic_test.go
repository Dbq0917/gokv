package gokv_test

import (
	"errors"
	"testing"

	"github.com/philippgille/gokv"
)

type storeWithoutAtomics struct{}

func (storeWithoutAtomics) Set(string, any) error         { return nil }
func (storeWithoutAtomics) Get(string, any) (bool, error) { return false, nil }
func (storeWithoutAtomics) Delete(string) error           { return nil }
func (storeWithoutAtomics) Close() error                  { return nil }

func TestAtomicHelpersRejectUnsupportedStores(t *testing.T) {
	store := storeWithoutAtomics{}

	err := gokv.Update(store, "key", new(int), func(bool) error { return nil })
	if !errors.Is(err, gokv.ErrAtomicUpdateNotSupported) {
		t.Fatalf("Update: expected ErrAtomicUpdateNotSupported, got %v", err)
	}

	_, err = gokv.CompareAndSwap(store, "key", 1, 2)
	if !errors.Is(err, gokv.ErrAtomicCompareAndSwapNotSupported) {
		t.Fatalf("CompareAndSwap: expected ErrAtomicCompareAndSwapNotSupported, got %v", err)
	}

	_, err = gokv.Increment(store, "key", 1)
	if !errors.Is(err, gokv.ErrAtomicIncrementNotSupported) {
		t.Fatalf("Increment: expected ErrAtomicIncrementNotSupported, got %v", err)
	}
}
