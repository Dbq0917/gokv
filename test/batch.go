package test

import (
	"errors"
	"strconv"
	"testing"

	"github.com/philippgille/gokv"
)

// batchSize is the number of key-value pairs written in the shared batch tests.
// It stays within badgerdb.DefaultMaxBatchSize so the same cases run unchanged
// on every backend that implements gokv.BatchWriter.
const batchSize = 10000

// TestSetMulti runs the shared batch-write test suite against the given store.
// The store must implement gokv.BatchWriter; the test fails otherwise,
// because the suite is only meant to be invoked by batch-capable backends.
//
// The same function is called from each backend's own test package, which makes
// the batch semantics identical across backends and prevents them from drifting.
func TestSetMulti(store gokv.Store, t *testing.T) {
	batchWriter, ok := store.(gokv.BatchWriter)
	if !ok {
		t.Fatalf("%T does not implement gokv.BatchWriter", store)
	}

	t.Run("large batch is fully written", func(t *testing.T) {
		testLargeBatch(store, batchWriter, t)
	})
	t.Run("invalid entry rolls back the whole batch", func(t *testing.T) {
		testAtomicRollback(store, batchWriter, t)
	})
	t.Run("empty and nil batch are a no-op", func(t *testing.T) {
		testEmptyBatch(batchWriter, t)
	})
}

// TestSetMultiDurability verifies that a successfully written batch survives
// closing and re-opening the store, i.e. it was persisted to disk rather than
// only buffered in memory. reopen must return a store backed by the same on-disk
// data as the passed store (same path/options).
func TestSetMultiDurability(store gokv.Store, reopen func() (gokv.Store, error), t *testing.T) {
	batchWriter, ok := store.(gokv.BatchWriter)
	if !ok {
		t.Fatalf("%T does not implement gokv.BatchWriter", store)
	}

	items := make(map[string]any, batchSize)
	for i := 0; i < batchSize; i++ {
		items[batchKey(i)] = Foo{Bar: "durable-" + strconv.Itoa(i)}
	}
	if err := batchWriter.SetMulti(items); err != nil {
		t.Fatalf("SetMulti failed: %v", err)
	}

	if err := store.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	reopened, err := reopen()
	if err != nil {
		t.Fatalf("reopening the store failed: %v", err)
	}
	defer func() { _ = reopened.Close() }()

	for i := 0; i < batchSize; i++ {
		actual := new(Foo)
		found, err := reopened.Get(batchKey(i), actual)
		if err != nil {
			t.Errorf("Get(%q) returned error: %v", batchKey(i), err)
		}
		if !found {
			t.Errorf("Get(%q) found nothing after reopening", batchKey(i))
			continue
		}
		if expected := "durable-" + strconv.Itoa(i); actual.Bar != expected {
			t.Errorf("Get(%q) = %q, want %q", batchKey(i), actual.Bar, expected)
		}
	}
}

// TestTrySetMultiUnsupported checks that TrySetMulti rejects a store without
// BatchWriter before writing anything, instead of silently doing a per-key loop.
func TestTrySetMultiUnsupported(store gokv.Store, t *testing.T) {
	if _, ok := store.(gokv.BatchWriter); ok {
		t.Fatalf("%T implements gokv.BatchWriter, but an unsupported store was expected", store)
	}

	err := gokv.TrySetMulti(store, map[string]any{"foo": "bar"})
	if !errors.Is(err, gokv.ErrBatchNotSupported) {
		t.Fatalf("expected gokv.ErrBatchNotSupported, got: %v", err)
	}

	// Nothing must have been written by the rejected call.
	found, err := store.Get("foo", new(string))
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if found {
		t.Error("a key was written even though the batch was reported as unsupported")
	}
}

func testLargeBatch(store gokv.Store, batchWriter gokv.BatchWriter, t *testing.T) {
	items := make(map[string]any, batchSize)
	for i := 0; i < batchSize; i++ {
		items[batchKey(i)] = Foo{Bar: strconv.Itoa(i)}
	}

	if err := batchWriter.SetMulti(items); err != nil {
		t.Fatalf("SetMulti failed: %v", err)
	}

	// Every single pair must be readable afterwards - a partial write fails here.
	for i := 0; i < batchSize; i++ {
		actual := new(Foo)
		found, err := store.Get(batchKey(i), actual)
		if err != nil {
			t.Errorf("Get(%q) returned error: %v", batchKey(i), err)
		}
		if !found {
			t.Errorf("Get(%q) found nothing, but the whole batch should have been stored", batchKey(i))
			continue
		}
		if actual.Bar != strconv.Itoa(i) {
			t.Errorf("Get(%q) = %q, want %q", batchKey(i), actual.Bar, strconv.Itoa(i))
		}
	}
}

func testAtomicRollback(store gokv.Store, batchWriter gokv.BatchWriter, t *testing.T) {
	items := map[string]any{
		batchKey(0): Foo{Bar: "before"},
	}
	if err := batchWriter.SetMulti(items); err != nil {
		t.Fatalf("seed SetMulti failed: %v", err)
	}

	// A nil value is invalid and must be rejected before the transaction opens.
	badBatch := map[string]any{
		batchKey(0): Foo{Bar: "after"},
		"":          Foo{Bar: "never stored"},
	}
	err := batchWriter.SetMulti(badBatch)
	if err == nil {
		t.Fatal("expected an error for the invalid batch, got nil")
	}

	// Atomicity: the pre-existing value must be unchanged and no new key may appear.
	actual := new(Foo)
	found, err := store.Get(batchKey(0), actual)
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if !found {
		t.Fatal("pre-existing key disappeared, but the failed batch must have rolled back")
	}
	if actual.Bar != "before" {
		t.Errorf("pre-existing value = %q, want %q (batch was not atomic)", actual.Bar, "before")
	}
}

func testEmptyBatch(batchWriter gokv.BatchWriter, t *testing.T) {
	if err := batchWriter.SetMulti(nil); err != nil {
		t.Errorf("SetMulti(nil) returned error: %v", err)
	}
	if err := batchWriter.SetMulti(map[string]any{}); err != nil {
		t.Errorf("SetMulti(empty map) returned error: %v", err)
	}
}

func batchKey(i int) string {
	return "batch-" + strconv.Itoa(i)
}
