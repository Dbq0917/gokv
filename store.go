package gokv

import "errors"

// ErrIterationNotSupported is returned by the Iterate method of gokv.Store implementations
// whose underlying storage doesn't allow listing keys, for example Memcached.
// It's a sentinel error, so you can check for it with errors.Is().
var ErrIterationNotSupported = errors.New("iteration is not supported by this gokv.Store implementation")

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
	// Iterate returns an iterator for the key-value pairs whose key starts with the given key prefix.
	// An empty prefix iterates over all key-value pairs in the store.
	// The iterator streams the entries one by one,
	// so you can process them without loading all key-value pairs into memory at once.
	// Writes that happen concurrently to the iteration must not lead to entries being returned
	// multiple times or entries that existed before the iteration being skipped,
	// and must not require the store to be locked as a whole.
	// Entries that are created or deleted while the iteration is in progress may or may not be returned.
	// The order in which entries are returned depends on the store implementation.
	// If the store implementation doesn't support iteration,
	// an error wrapping ErrIterationNotSupported is returned instead of an empty result.
	Iterate(keyPrefix string) (Iterator, error)
}

// Iterator iterates over the key-value pairs of a gokv.Store, one entry at a time.
// It's not safe for concurrent use.
type Iterator interface {
	// Next advances the iterator to the next entry.
	// It returns false when there are no more entries or when an error occurred.
	// After Next returned false, Err can be checked for the error that stopped the iteration.
	Next() bool
	// Key returns the key of the entry the iterator currently points to.
	// It must only be called after Next returned true.
	Key() string
	// Value unmarshals the value of the entry the iterator currently points to into v.
	// It must only be called after Next returned true.
	// As with the Get method of the Store interface, v must be a non-nil pointer.
	// If the entry has been deleted concurrently after the iterator advanced to it,
	// found is false.
	Value(v any) (found bool, err error)
	// Err returns the error that caused Next to return false, if any.
	Err() error
	// Close releases the resources held by the iterator, for example open database transactions.
	// It must be called when the iterator is not needed anymore,
	// regardless of whether the iteration finished or an error occurred.
	// Calling Close multiple times is not an error.
	Close() error
}
