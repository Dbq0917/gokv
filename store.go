package gokv

// Store is an abstraction for different key-value store implementations.
// A store must be able to store, retrieve and delete key-value pairs,
// with the key being a string and the value being any Go interface{}.
type Store interface {
	// Set stores the given value for the given key.
	// The implementation automatically marshalls the value.
	// The marshalling format depends on the implementation. It can be JSON, gob etc.
	// The key must not be "" and the value must not be nil.
	Set(k string, v any) error
	// Get retrieves the value for the given key.
	// The implementation automatically unmarshalls the value.
	// The unmarshalling source depends on the implementation. It can be JSON, gob etc.
	// The automatic unmarshalling requires a pointer to an object of the correct type
	// being passed as parameter.
	// In case of a struct the Get method will populate the fields of the object
	// that the passed pointer points to with the values of the retrieved object's values.
	// If no value is found it returns (false, nil).
	// The key must not be "" and the pointer must not be nil.
	Get(k string, v any) (found bool, err error)
	// Delete deletes the stored value for the given key.
	// Deleting a non-existing key-value pair does NOT lead to an error.
	// The key must not be "".
	Delete(k string) error
	// Close must be called when the work with the key-value store is done.
	// Most (if not all) implementations are meant to be used long-lived,
	// so only call Close() at the very end.
	// Depending on the store implementation it might do one or more of the following:
	// Make sure all pending updates make their way to disk,
	// finish open transactions,
	// close the file handle to an embedded DB,
	// close the connection to the DB server,
	// release any open resources,
	// etc.
	// Some implementation might not need the store to be closed,
	// but as long as you work with the gokv.Store interface you never know which implementation
	// is passed to your method, so you should always call it.
	Close() error
}

// BatchDeleter is an optional interface that a gokv.Store implementation
// can implement to delete multiple key-value pairs in a single operation.
//
// The operation MUST be atomic: Either all given key-value pairs are deleted,
// or none of them are. If the method returns an error, no key-value pair
// was deleted. This makes the method safe to use for cleanup jobs that must
// not leave a half-deleted state behind when the backend fails midway.
//
// Implementations MUST NOT emulate atomicity on backends that don't provide
// it natively (for example via a single transaction or a single atomic batch
// write), because emulations like locking the store and deleting key-value
// pairs one by one would still leave a partial state behind on failure,
// and holding long locks can overload an already slow backend.
// An implementation that cannot guarantee atomicity must simply not
// implement this interface.
//
// Because not every gokv.Store implementation supports atomic batch deletes,
// callers must check for support before relying on it, for example:
//
//	if batchDeleter, ok := store.(gokv.BatchDeleter); ok {
//		err := batchDeleter.DeleteBatch(keys) // atomic
//	} else {
//		// The store doesn't support atomic batch deletes.
//		// Fall back to deleting key-value pairs one by one
//		// and handle partial failures yourself.
//	}
//
// Note: Implementations typically execute the batch as a single transaction,
// so very large batches may fail as a whole (for example when exceeding a
// maximum transaction size). Atomicity still holds in that case. Callers may
// split a huge batch into smaller chunks, but then atomicity only holds
// per chunk.
type BatchDeleter interface {
	// DeleteBatch deletes the stored values for the given keys.
	// Either all key-value pairs are deleted or none of them are.
	// Deleting non-existing key-value pairs does NOT lead to an error.
	// An empty slice is a no-op and does NOT lead to an error.
	// Keys must not be "". If any key is "", an error is returned
	// and no key-value pair is deleted.
	DeleteBatch(keys []string) error
}
