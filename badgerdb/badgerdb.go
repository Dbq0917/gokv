package badgerdb

import (
	"github.com/dgraph-io/badger"

	"github.com/philippgille/gokv"
	"github.com/philippgille/gokv/encoding"
	"github.com/philippgille/gokv/util"
)

// Store is a gokv.Store implementation for BadgerDB.
type Store struct {
	db    *badger.DB
	codec encoding.Codec
}

// Set stores the given value for the given key.
// Values are automatically marshalled to JSON or gob (depending on the configuration).
// The key must not be "" and the value must not be nil.
func (s Store) Set(k string, v any) error {
	if err := util.CheckKeyAndValue(k, v); err != nil {
		return err
	}

	// First turn the passed object into something that BadgerDB can handle
	data, err := s.codec.Marshal(v)
	if err != nil {
		return err
	}

	err = s.db.Update(func(txn *badger.Txn) error {
		return txn.Set([]byte(k), data)
	})
	if err != nil {
		return err
	}
	return nil
}

// Get retrieves the stored value for the given key.
// You need to pass a pointer to the value, so in case of a struct
// the automatic unmarshalling can populate the fields of the object
// that v points to with the values of the retrieved object's values.
// If no value is found it returns (false, nil).
// The key must not be "" and the pointer must not be nil.
func (s Store) Get(k string, v any) (found bool, err error) {
	if err := util.CheckKeyAndValue(k, v); err != nil {
		return false, err
	}

	var data []byte
	err = s.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte(k))
		if err != nil {
			return err
		}
		// item.Value() is only valid within the transaction.
		// We can either copy it ourselves or use the ValueCopy() method.
		// TODO: Benchmark if it's faster to copy + close tx,
		// or to keep the tx open until unmarshalling is done.
		data, err = item.ValueCopy(nil)
		if err != nil {
			return err
		}
		return nil
	})
	// If no value was found return false
	if err == badger.ErrKeyNotFound {
		return false, nil
	} else if err != nil {
		return false, err
	}

	return true, s.codec.Unmarshal(data, v)
}

// Delete deletes the stored value for the given key.
// Deleting a non-existing key-value pair does NOT lead to an error.
// The key must not be "".
func (s Store) Delete(k string) error {
	if err := util.CheckKey(k); err != nil {
		return err
	}

	return s.db.Update(func(txn *badger.Txn) error {
		return txn.Delete([]byte(k))
	})
}

// Close closes the store.
// It must be called to make sure that all pending updates make their way to disk.
func (s Store) Close() error {
	return s.db.Close()
}

// Iterate returns an iterator for the key-value pairs whose key starts with the given key prefix.
// An empty prefix iterates over all key-value pairs in the store.
// The iterator works on a consistent snapshot of the store
// (a BadgerDB read transaction, which doesn't block writers),
// so entries that are created or deleted while the iteration is in progress are not returned,
// and entries are never returned multiple times or skipped.
func (s Store) Iterate(keyPrefix string) (gokv.Iterator, error) {
	txn := s.db.NewTransaction(false)

	options := badger.DefaultIteratorOptions
	options.Prefix = []byte(keyPrefix)

	return &iterator{
		txn:      txn,
		iterator: txn.NewIterator(options),
		codec:    s.codec,
	}, nil
}

type iterator struct {
	txn      *badger.Txn
	iterator *badger.Iterator
	codec    encoding.Codec
	key      []byte
	value    []byte
	valueErr error
	iterated bool
}

// Next advances the iterator to the next entry.
func (i *iterator) Next() bool {
	if !i.iterated {
		i.iterator.Rewind()
		i.iterated = true
	} else {
		i.iterator.Next()
	}
	if !i.iterator.Valid() {
		return false
	}
	item := i.iterator.Item()
	i.key = item.KeyCopy(i.key[:0])
	// The value is only valid within the transaction, so we copy it.
	i.value, i.valueErr = item.ValueCopy(i.value[:0])
	return true
}

// Key returns the key of the entry the iterator currently points to.
func (i *iterator) Key() string {
	return string(i.key)
}

// Value unmarshals the value of the entry the iterator currently points to into v.
func (i *iterator) Value(v any) (found bool, err error) {
	if err := util.CheckVal(v); err != nil {
		return false, err
	}
	if i.valueErr != nil {
		return false, i.valueErr
	}

	return true, i.codec.Unmarshal(i.value, v)
}

// Err returns the error that occurred while copying the current value, if any.
func (i *iterator) Err() error {
	return i.valueErr
}

// Close releases the resources held by the iterator and discards the read transaction.
func (i *iterator) Close() error {
	i.iterator.Close()
	i.txn.Discard()
	return nil
}

// Options are the options for the BadgerDB store.
type Options struct {
	// Directory for storing the DB files.
	// Optional ("BadgerDB" by default).
	Dir string
	// Encoding format.
	// Optional (encoding.JSON by default).
	Codec encoding.Codec
}

// DefaultOptions is an Options object with default values.
// Dir: "BadgerDB", Codec: encoding.JSON
var DefaultOptions = Options{
	Dir:   "BadgerDB",
	Codec: encoding.JSON,
}

// NewStore creates a new BadgerDB store.
// Note: BadgerDB uses an exclusive write lock on the database directory so it cannot be shared by multiple processes.
// So when creating multiple clients you should always use a new database directory (by setting a different Path in the options).
//
// You must call the Close() method on the store when you're done working with it.
func NewStore(options Options) (Store, error) {
	result := Store{}

	// Set default values
	if options.Dir == "" {
		options.Dir = DefaultOptions.Dir
	}
	if options.Codec == nil {
		options.Codec = DefaultOptions.Codec
	}

	// Open the Badger database located in the options.Dir directory.
	// It will be created if it doesn't exist.
	opts := badger.DefaultOptions(options.Dir)
	db, err := badger.Open(opts)
	if err != nil {
		return result, err
	}

	result.db = db
	result.codec = options.Codec

	return result, nil
}
