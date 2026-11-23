package gokv

import "errors"

// ErrAtomicUpdateNotSupported is returned when a Store does not implement
// the optional AtomicUpdater interface.
var ErrAtomicUpdateNotSupported = errors.New(
	"the store does not support atomic updates (it doesn't implement gokv.AtomicUpdater)",
)

// ErrAtomicCompareAndSwapNotSupported is returned when a Store does not
// implement the optional AtomicComparer interface.
var ErrAtomicCompareAndSwapNotSupported = errors.New(
	"the store does not support compare-and-swap (it doesn't implement gokv.AtomicComparer)",
)

// ErrAtomicIncrementNotSupported is returned when a Store does not implement
// the optional AtomicIncrementer interface.
var ErrAtomicIncrementNotSupported = errors.New(
	"the store does not support atomic increments (it doesn't implement gokv.AtomicIncrementer)",
)

// ErrAtomicUpdateRetryExhausted is returned when a store gives up after too
// many consecutive optimistic-concurrency conflicts.
var ErrAtomicUpdateRetryExhausted = errors.New(
	"atomic update retry limit exceeded due to repeated concurrent modifications",
)

// UpdateFunc modifies the value passed to AtomicUpdater.Update.
//
// found reports whether the key existed before the update. If it didn't,
// the value was reset to its zero value before this function was called.
//
// An implementation may call UpdateFunc more than once when another writer
// changes the key concurrently. The function must therefore only mutate the
// supplied value and must not rely on external side effects. Returning an
// error aborts the update without writing anything.
type UpdateFunc func(found bool) error

// AtomicUpdater is an optional interface for stores that can perform an
// atomic read-modify-write operation on a single key.
//
// Update must not lose concurrent updates: if another writer changes the key
// between the read and the write, the implementation detects the conflict and
// retries using the latest value. Implementations must not emulate this by
// simply calling Get followed by Set, because that has the lost-update problem
// this interface exists to solve.
type AtomicUpdater interface {
	// Update reads the value for k into v, calls update, and stores v again.
	//
	// v must be a non-nil pointer to the value's type. The update function may
	// be invoked more than once under contention.
	Update(k string, v any, update UpdateFunc) error
}

// AtomicComparer is an optional interface for stores that can atomically
// replace a value only if it still equals the expected value.
type AtomicComparer interface {
	// CompareAndSwap stores newValue for k only if the current value equals
	// oldValue. A nil oldValue means that k must not exist.
	//
	// It returns true only when the new value was stored. A value mismatch is
	// reported as (false, nil), not as an error. newValue must not be nil.
	CompareAndSwap(k string, oldValue, newValue any) (swapped bool, err error)
}

// AtomicIncrementer is an optional interface for stores that can atomically
// add delta to a signed 64-bit integer stored under a key.
type AtomicIncrementer interface {
	// Increment adds delta to the value for k and returns the new value.
	// A missing key is treated as zero.
	Increment(k string, delta int64) (value int64, err error)
}

// Update performs an atomic read-modify-write when store implements
// AtomicUpdater. Otherwise it returns ErrAtomicUpdateNotSupported without
// reading or writing anything.
func Update(store Store, k string, v any, update UpdateFunc) error {
	updater, ok := store.(AtomicUpdater)
	if !ok {
		return ErrAtomicUpdateNotSupported
	}
	return updater.Update(k, v, update)
}

// CompareAndSwap performs an atomic compare-and-swap when store implements
// AtomicComparer. Otherwise it returns ErrAtomicCompareAndSwapNotSupported.
func CompareAndSwap(store Store, k string, oldValue, newValue any) (swapped bool, err error) {
	comparer, ok := store.(AtomicComparer)
	if !ok {
		return false, ErrAtomicCompareAndSwapNotSupported
	}
	return comparer.CompareAndSwap(k, oldValue, newValue)
}

// Increment atomically adds delta to an integer value when store implements
// AtomicIncrementer. Otherwise it returns ErrAtomicIncrementNotSupported.
func Increment(store Store, k string, delta int64) (value int64, err error) {
	incrementer, ok := store.(AtomicIncrementer)
	if !ok {
		return 0, ErrAtomicIncrementNotSupported
	}
	return incrementer.Increment(k, delta)
}
