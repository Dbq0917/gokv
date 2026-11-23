package gokv

// ByteStore is an optional capability interface that a gokv.Store implementation
// can provide in addition to the basic Set/Get/Delete/Close methods.
//
// It exposes the raw, already marshalled bytes that are stored under a key,
// without the marshalling round-trip that Set/Get perform. This is mainly used
// for backend-to-backend data migration, where values must be copied verbatim
// and independent of the concrete encoding.Codec that was used when writing them.
//
// A type assertion can be used to detect whether a store supports it:
//
//	bs, ok := store.(gokv.ByteStore)
type ByteStore interface {
	// SetBytes stores the given raw bytes for the given key, replacing any
	// existing value. The key must not be "". A nil or empty data slice is
	// treated like a regular (empty) value and must still be retrievable.
	SetBytes(k string, data []byte) error
	// GetBytes returns the raw bytes stored under the given key.
	// If no value is found it returns (nil, false, nil), mirroring gokv.Store.Get.
	GetBytes(k string) (data []byte, found bool, err error)
}

// IterStore is an optional capability interface that a gokv.Store implementation
// can provide to enumerate all keys that currently exist in the store.
//
// It is used during migration to discover every key in the source store without
// having to know the keys up front. The returned keys are a snapshot taken at
// the time of the call; keys added afterwards are not required to be included.
type IterStore interface {
	// Keys returns all keys that currently exist in the store.
	// The order of the returned keys is not specified.
	Keys() ([]string, error)
}
