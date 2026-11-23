package migration_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/philippgille/gokv"
	"github.com/philippgille/gokv/bbolt"
	"github.com/philippgille/gokv/migration"
	"github.com/philippgille/gokv/syncmap"
)

// record is the value type migrated in the tests. It is intentionally a plain
// struct so JSON marshalling round-trips without surprises.
type record struct {
	Seq  int
	Note string
}

// TestMigrateThenRestart is the migration regression test: move a batch of
// in-memory data to the on-disk store, switch over, then close and re-open the
// disk store (a restart) and assert every value survived. This is the scenario
// that previously lost the not-yet-flushed increments.
func TestMigrateThenRestart(t *testing.T) {
	source, target, path := newPair(t)

	// Pre-existing batch: the data that was already in memory on migration day.
	const batchSize = 200
	for i := 0; i < batchSize; i++ {
		key := fmt.Sprintf("key-%04d", i)
		if err := source.Set(key, record{Seq: i, Note: "batch"}); err != nil {
			t.Fatalf("seed source: %v", err)
		}
	}

	dual, err := migration.NewDualStore(source, target)
	if err != nil {
		t.Fatalf("NewDualStore: %v", err)
	}

	// Copy the pre-existing data and verify. Re-run while verification reports
	// differences to absorb keys written concurrently during the copy.
	if err := copyUntilVerified(t, dual); err != nil {
		t.Fatalf("initial copy/verify: %v", err)
	}

	// The switch must only happen after verification confirms everything is
	// durably present and identical in the target.
	if err := dual.Commit(); err != nil {
		t.Fatalf("Commit after successful verify: %v", err)
	}
	if phase := dual.Phase(); phase != migration.Switched {
		t.Fatalf("phase after Commit = %s, want Switched", phase)
	}

	// Reads now come from the target.
	got := new(record)
	found, err := dual.Get("key-0007", got)
	if err != nil {
		t.Fatalf("Get after switch: %v", err)
	}
	if !found || got.Seq != 7 {
		t.Fatalf("Get after switch = %+v (found=%v), want seq 7", got, found)
	}

	// Simulate a process restart: drop both handles and re-open the disk store.
	if err := dual.Close(); err != nil {
		t.Fatalf("close before restart: %v", err)
	}
	reopened, err := bbolt.NewStore(bbolt.Options{Path: path})
	if err != nil {
		t.Fatalf("re-open target after restart: %v", err)
	}
	defer func() { _ = reopened.Close() }()

	for i := 0; i < batchSize; i++ {
		key := fmt.Sprintf("key-%04d", i)
		actual := new(record)
		found, err := reopened.Get(key, actual)
		if err != nil {
			t.Fatalf("Get %q after restart: %v", key, err)
		}
		if !found {
			t.Fatalf("key %q missing after restart", key)
		}
		if actual.Seq != i || actual.Note != "batch" {
			t.Fatalf("key %q = %+v after restart, want seq %d note batch", key, actual, i)
		}
	}
}

// TestConcurrentIncrementsNotLost keeps writing through the DualStore while the
// background copy runs, modelling migration-day increments. After Commit and a
// restart every increment must be present in the disk store.
func TestConcurrentIncrementsNotLost(t *testing.T) {
	source, target, path := newPair(t)

	dual, err := migration.NewDualStore(source, target)
	if err != nil {
		t.Fatalf("NewDualStore: %v", err)
	}

	const writers = 8
	const perWriter = 50

	var wg sync.WaitGroup
	wg.Add(writers)
	for w := 0; w < writers; w++ {
		go func(w int) {
			defer wg.Done()
			for i := 0; i < perWriter; i++ {
				key := fmt.Sprintf("w%d-k%03d", w, i)
				if err := dual.Set(key, record{Seq: w*perWriter + i, Note: "incr"}); err != nil {
					t.Errorf("concurrent Set %q: %v", key, err)
					return
				}
			}
		}(w)
	}

	// Copy in parallel with the writes. A first pass may see a moving target,
	// which is exactly why Commit verifies and the loop re-runs.
	copyDone := make(chan struct{})
	go func() {
		defer close(copyDone)
		if _, err := dual.CopyAll(); err != nil {
			t.Errorf("CopyAll: %v", err)
		}
	}()
	<-copyDone
	wg.Wait()

	if err := copyUntilVerified(t, dual); err != nil {
		t.Fatalf("verify after concurrent writes: %v", err)
	}
	if err := dual.Commit(); err != nil {
		t.Fatalf("Commit after concurrent writes: %v", err)
	}

	if err := dual.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	reopened, err := bbolt.NewStore(bbolt.Options{Path: path})
	if err != nil {
		t.Fatalf("re-open after restart: %v", err)
	}
	defer func() { _ = reopened.Close() }()

	for w := 0; w < writers; w++ {
		for i := 0; i < perWriter; i++ {
			key := fmt.Sprintf("w%d-k%03d", w, i)
			actual := new(record)
			found, err := reopened.Get(key, actual)
			if err != nil {
				t.Fatalf("Get %q: %v", key, err)
			}
			if !found {
				t.Fatalf("increment %q lost after migration + restart", key)
			}
			if actual.Note != "incr" || actual.Seq != w*perWriter+i {
				t.Fatalf("increment %q = %+v, want seq %d", key, actual, w*perWriter+i)
			}
		}
	}
}

// TestCommitBlockedUntilVerified proves the ordering guard: Commit refuses to
// switch while the target is missing data, so a cut-over can never happen
// before everything is confirmed written.
func TestCommitBlockedUntilVerified(t *testing.T) {
	source, target, _ := newPair(t)

	if err := source.Set("only-in-source", record{Seq: 1, Note: "x"}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	dual, err := migration.NewDualStore(source, target)
	if err != nil {
		t.Fatalf("NewDualStore: %v", err)
	}

	// Nothing was copied yet, so verification must fail and Commit must not switch.
	if err := dual.Verify(); err == nil {
		t.Fatal("Verify should fail while target is empty")
	}
	if err := dual.Commit(); err == nil {
		t.Fatal("Commit must fail while data is missing in the target")
	} else {
		var verr *migration.VerificationError
		if !errors.As(err, &verr) {
			t.Fatalf("Commit error = %v, want a *VerificationError wrapper", err)
		}
		if len(verr.Missing) != 1 || verr.Missing[0] != "only-in-source" {
			t.Fatalf("missing keys = %v, want [only-in-source]", verr.Missing)
		}
	}
	if phase := dual.Phase(); phase != migration.DualWrite {
		t.Fatalf("phase after rejected Commit = %s, want DualWrite", phase)
	}

	// After copying and verifying, the switch is allowed.
	if err := copyUntilVerified(t, dual); err != nil {
		t.Fatalf("copy/verify: %v", err)
	}
	if err := dual.Commit(); err != nil {
		t.Fatalf("Commit after catch-up: %v", err)
	}
}

// TestRollback verifies that after a switch the primary can be moved back to
// the source without data loss, because the source is never destroyed.
func TestRollback(t *testing.T) {
	source, target, _ := newPair(t)

	if err := source.Set("k", record{Seq: 9, Note: "n"}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	dual, err := migration.NewDualStore(source, target)
	if err != nil {
		t.Fatalf("NewDualStore: %v", err)
	}
	if err := copyUntilVerified(t, dual); err != nil {
		t.Fatalf("copy/verify: %v", err)
	}
	if err := dual.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	if err := dual.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if phase := dual.Phase(); phase != migration.RolledBack {
		t.Fatalf("phase after Rollback = %s, want RolledBack", phase)
	}

	// Reads come from the source again and the value is still there.
	actual := new(record)
	found, err := dual.Get("k", actual)
	if err != nil {
		t.Fatalf("Get after rollback: %v", err)
	}
	if !found || actual.Seq != 9 {
		t.Fatalf("Get after rollback = %+v (found=%v), want seq 9", actual, found)
	}

	// New writes still reach the target so the migration can be retried.
	if err := dual.Set("k2", record{Seq: 10, Note: "after-rb"}); err != nil {
		t.Fatalf("Set after rollback: %v", err)
	}
	diskVal := new(record)
	found, err = target.Get("k2", diskVal)
	if err != nil {
		t.Fatalf("target Get after rollback: %v", err)
	}
	if !found || diskVal.Seq != 10 {
		t.Fatalf("target missing post-rollback write, got %+v found=%v", diskVal, found)
	}

	if err := dual.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

// TestCapabilities asserts the concrete stores satisfy the optional interfaces
// the migration relies on, guarding against accidental regressions.
func TestCapabilities(t *testing.T) {
	var _ gokv.ByteStore = syncmap.Store{}
	var _ gokv.IterStore = syncmap.Store{}
	var _ gokv.ByteStore = bbolt.Store{}
	var _ gokv.IterStore = bbolt.Store{}
}

// copyUntilVerified runs CopyAll followed by Verify until the stores converge.
// A bounded number of attempts keeps the test deterministic even though writes
// can land between a copy and its verification.
func copyUntilVerified(t *testing.T, dual *migration.DualStore) error {
	t.Helper()
	const maxAttempts = 10
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if _, err := dual.CopyAll(); err != nil {
			return err
		}
		lastErr = dual.Verify()
		if lastErr == nil {
			return nil
		}
	}
	return fmt.Errorf("stores did not converge after %d attempts: %w", maxAttempts, lastErr)
}

// newPair builds an in-memory source and an on-disk target in a temp directory,
// returning the stores plus the DB path used for restart tests.
func newPair(t *testing.T) (syncmap.Store, bbolt.Store, string) {
	t.Helper()

	dir, err := os.MkdirTemp(os.TempDir(), "gokv-migration")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	path := filepath.Join(dir, "target.db")
	source := syncmap.NewStore(syncmap.DefaultOptions)
	target, err := bbolt.NewStore(bbolt.Options{Path: path})
	if err != nil {
		t.Fatalf("bbolt.NewStore: %v", err)
	}
	return source, target, path
}
