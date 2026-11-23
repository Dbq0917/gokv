package gokv

import "errors"

// ErrBatchNotSupported is returned by TrySetMulti when the passed Store
// does not implement the BatchWriter interface.
// It is returned before any key-value pair is written,
// so callers can detect the missing capability up front
// instead of discovering a partial write after the fact.
var ErrBatchNotSupported = errors.New(
	"the store does not support atomic batch writes (it doesn't implement gokv.BatchWriter)",
)

// ErrBatchTooLarge is returned by SetMulti when the number of key-value pairs
// exceeds the limit a backend can write in a single atomic transaction.
// Like ErrBatchNotSupported it is returned before anything is written.
// The caller must split the batch into smaller chunks and retry each chunk.
var ErrBatchTooLarge = errors.New(
	"the batch is larger than the backend's atomic write limit",
)

// BatchWriter is an optional interface for Stores that can write multiple
// key-value pairs in one atomic, durable operation.
//
// In contrast to calling Set repeatedly in a loop, a SetMulti call has
// well-defined semantics that every implementation must honor:
//
//   - Atomic: either every key-value pair is stored or none of them is.
//     A returned error therefore means the store is left completely untouched
//     by this call - there is no "half succeeded, half failed" outcome.
//   - Durable: a successful return means the whole batch was committed to the
//     backend's durable storage (for example fsynced to disk), not just held in
//     an in-memory buffer. A crash after a successful call does not lose data.
//
// Because the whole batch either succeeds or fails, SetMulti is safe to retry
// after an error: retrying never duplicates or partially applies data.
//
// Not every backend is able to provide these guarantees, so BatchWriter is
// intentionally NOT part of the Store interface. Backends which cannot make
// the atomicity and durability promises must not emulate SetMulti by looping
// over Set, because that would silently re-introduce partial writes.
// Callers check for the capability before writing via a type assertion or
// TrySetMulti, and unsupported backends are rejected before any data is touched.
//
// An empty or nil map is a valid no-op that returns nil without opening a
// transaction. Each key must not be "" and each value must not be nil;
// invalid entries lead to an error with nothing being written.
type BatchWriter interface {
	SetMulti(items map[string]any) error
}

// TrySetMulti writes a batch of key-value pairs atomically and durably
// when the given Store implements BatchWriter.
//
// If the Store doesn't support atomic batch writes it returns
// ErrBatchNotSupported without writing anything, letting the caller handle
// the unsupported backend explicitly instead of assuming the batch succeeded.
//
// On a supporting store the call is forwarded directly to SetMulti,
// so backends with a native batch implementation keep their speed advantage -
// there is no generic fallback that writes entries one by one.
func TrySetMulti(store Store, items map[string]any) error {
	batchWriter, ok := store.(BatchWriter)
	if !ok {
		return ErrBatchNotSupported
	}
	return batchWriter.SetMulti(items)
}
