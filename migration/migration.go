// Package migration provides a zero-downtime, reversible way to move the data
// of a gokv.Store from one backend to another (for example from an in-memory
// store like syncmap to a persistent, on-disk store like bbolt).
//
// The migration is driven by a DualStore that wraps the old ("source") and the
// new ("target") store and moves through three phases:
//
//   - DualWrite: every write is applied to both stores while reads still come
//     from the source. The service keeps running normally the whole time.
//   - After CopyAll copied the pre-existing data and Verify confirmed that
//     every key exists in the target with identical bytes, Commit switches the
//     read/write primary to the target. Commit refuses to switch while the
//     stores are not in sync, so a switch can never happen before all data is
//     durably written and checked.
//   - Until the source is explicitly closed, Rollback can switch the primary
//     back to the source at any time, because the source is never mutated in a
//     destructive way and keeps serving as the fallback.
package migration

import (
	"errors"
	"fmt"
	"sync"

	"github.com/philippgille/gokv"
)

// Phase describes which role the source and target store currently play.
type Phase int

const (
	// DualWrite is the initial phase. Writes go to both stores, reads come
	// from the source. This is the phase in which the background copy and the
	// verification happen, so the service stays online throughout.
	DualWrite Phase = iota
	// Switched means the target is now the read/write primary. The source is
	// kept open and untouched so Rollback remains possible.
	Switched
	// RolledBack means the source is the read/write primary again. The target
	// still receives writes so a later re-attempt of the migration is possible.
	RolledBack
)

// String returns a human-readable name for the phase, useful for logging.
func (p Phase) String() string {
	switch p {
	case DualWrite:
		return "DualWrite"
	case Switched:
		return "Switched"
	case RolledBack:
		return "RolledBack"
	default:
		return fmt.Sprintf("Phase(%d)", int(p))
	}
}

var (
	// ErrUnsupportedCapability is returned when a store is missing an optional
	// capability (gokv.ByteStore or gokv.IterStore) that the migration needs.
	ErrUnsupportedCapability = errors.New("store doesn't implement a required optional capability")
	// ErrPhase is returned when an operation is not valid in the current phase.
	ErrPhase = errors.New("operation not valid in the current migration phase")
)

// VerificationError reports the differences that Verify found between the
// source and the target store. A non-nil error of this type means the data is
// not in sync and Commit must not switch the primary.
type VerificationError struct {
	// Missing contains keys that exist in the source but not in the target.
	Missing []string
	// Different contains keys whose bytes differ between source and target.
	Different []string
	// Extra contains keys that exist in the target but not in the source.
	Extra []string
}

// Error implements the error interface.
func (e *VerificationError) Error() string {
	return fmt.Sprintf(
		"migration verification failed: %d missing, %d different, %d extra key(s)",
		len(e.Missing), len(e.Different), len(e.Extra),
	)
}

// DualStore wraps a source store and a target store and coordinates the
// migration between them. It implements gokv.Store, so application code can
// keep using the ordinary Set/Get/Delete methods while the migration runs.
//
// The zero value is not usable; create one with NewDualStore.
type DualStore struct {
	source gokv.Store
	target gokv.Store

	lock  sync.RWMutex
	phase Phase
}

// NewDualStore creates a DualStore in the DualWrite phase.
// Both stores must implement gokv.ByteStore and the source must additionally
// implement gokv.IterStore, otherwise ErrUnsupportedCapability is returned.
func NewDualStore(source, target gokv.Store) (*DualStore, error) {
	if _, ok := source.(gokv.ByteStore); !ok {
		return nil, fmt.Errorf("%w: source is no gokv.ByteStore", ErrUnsupportedCapability)
	}
	if _, ok := target.(gokv.ByteStore); !ok {
		return nil, fmt.Errorf("%w: target is no gokv.ByteStore", ErrUnsupportedCapability)
	}
	if _, ok := source.(gokv.IterStore); !ok {
		return nil, fmt.Errorf("%w: source is no gokv.IterStore", ErrUnsupportedCapability)
	}
	return &DualStore{
		source: source,
		target: target,
		phase:  DualWrite,
	}, nil
}

// Phase returns the current migration phase.
func (d *DualStore) Phase() Phase {
	d.lock.RLock()
	defer d.lock.RUnlock()
	return d.phase
}

// Source returns the wrapped source store. It is exposed so callers can close
// it themselves once they are confident the migration is final and no longer
// need the rollback path.
func (d *DualStore) Source() gokv.Store {
	return d.source
}

// Target returns the wrapped target store.
func (d *DualStore) Target() gokv.Store {
	return d.target
}

// Set stores the value in the primary store and, while the primary isn't the
// target alone, also in the secondary one. Both stores therefore converge even
// while the migration is in progress, without taking the service offline.
func (d *DualStore) Set(k string, v any) error {
	d.lock.RLock()
	defer d.lock.RUnlock()

	primary, secondary := d.primaryAndSecondary()
	if err := primary.Set(k, v); err != nil {
		return err
	}
	if secondary != nil {
		if err := secondary.Set(k, v); err != nil {
			return err
		}
	}
	return nil
}

// Get retrieves the value from the current primary store.
func (d *DualStore) Get(k string, v any) (bool, error) {
	d.lock.RLock()
	primary, _ := d.primaryAndSecondary()
	d.lock.RUnlock()
	return primary.Get(k, v)
}

// Delete removes the key from both stores so they don't drift apart.
func (d *DualStore) Delete(k string) error {
	d.lock.RLock()
	defer d.lock.RUnlock()

	primary, secondary := d.primaryAndSecondary()
	if err := primary.Delete(k); err != nil {
		return err
	}
	if secondary != nil {
		if err := secondary.Delete(k); err != nil {
			return err
		}
	}
	return nil
}

// Close closes both the source and the target store.
func (d *DualStore) Close() error {
	var errs []error
	if err := d.source.Close(); err != nil {
		errs = append(errs, fmt.Errorf("closing source: %w", err))
	}
	if err := d.target.Close(); err != nil {
		errs = append(errs, fmt.Errorf("closing target: %w", err))
	}
	return errors.Join(errs...)
}

// primaryAndSecondary returns the current primary store and the store that
// must be kept in sync as a secondary. The secondary is nil once the primary
// is the only store that should be touched.
//
// Callers must hold at least the read lock.
func (d *DualStore) primaryAndSecondary() (primary gokv.Store, secondary gokv.Store) {
	switch d.phase {
	case Switched:
		// Target owns traffic; the source is retained only for rollback.
		return d.target, nil
	case RolledBack:
		// Source owns traffic again, but the target keeps receiving writes so
		// the migration can be retried without losing new increments.
		return d.source, d.target
	default: // DualWrite
		return d.source, d.target
	}
}
