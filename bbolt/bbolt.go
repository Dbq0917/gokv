package bbolt

import (
	"bytes"
	"errors"

	bolt "go.etcd.io/bbolt"

	"github.com/philippgille/gokv"
	"github.com/philippgille/gokv/encoding"
	"github.com/philippgille/gokv/util"
)

// Store is a gokv.Store implementation for bbolt (formerly known as Bolt / Bolt DB).
type Store struct {
	db         *bolt.DB
	bucketName string
	codec      encoding.Codec
}

// Set stores the given value for the given key.
// Values are automatically marshalled to JSON or gob (depending on the configuration).
// The key must not be "" and the value must not be nil.
func (s Store) Set(k string, v any) error {
	if err := util.CheckKeyAndValue(k, v); err != nil {
		return err
	}

	// First turn the passed object into something that bbolt can handle
	data, err := s.codec.Marshal(v)
	if err != nil {
		return err
	}

	err = s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(s.bucketName))
		return b.Put([]byte(k), data)
	})
	if err != nil {
		return err
	}
	return nil
}

// Update atomically reads, modifies and writes the value for the given key.
//
// The read and write happen in one bbolt write transaction, so concurrent
// updates are serialized by the database.
func (s Store) Update(k string, v any, update gokv.UpdateFunc) error {
	if err := util.CheckKey(k); err != nil {
		return err
	}
	if err := util.CheckPointer(v); err != nil {
		return err
	}
	if update == nil {
		return errors.New("the update function is nil")
	}

	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(s.bucketName))
		data := b.Get([]byte(k))
		found := data != nil

		if err := util.ResetPointer(v); err != nil {
			return err
		}
		if found {
			if err := s.codec.Unmarshal(data, v); err != nil {
				return err
			}
		}
		if err := update(found); err != nil {
			return err
		}

		newData, err := s.codec.Marshal(v)
		if err != nil {
			return err
		}
		return b.Put([]byte(k), newData)
	})
}

// CompareAndSwap atomically replaces the value for k if it matches oldValue.
// A nil oldValue means that k must not exist.
func (s Store) CompareAndSwap(k string, oldValue, newValue any) (swapped bool, err error) {
	if err := util.CheckKeyAndValue(k, newValue); err != nil {
		return false, err
	}
	if oldValue != nil {
		if err := util.CheckVal(oldValue); err != nil {
			return false, err
		}
	}

	var oldData []byte
	if oldValue != nil {
		oldData, err = s.codec.Marshal(oldValue)
		if err != nil {
			return false, err
		}
	}
	newData, err := s.codec.Marshal(newValue)
	if err != nil {
		return false, err
	}

	err = s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(s.bucketName))
		current := b.Get([]byte(k))
		found := current != nil
		if oldValue == nil {
			if found {
				return nil
			}
		} else if !found || !bytes.Equal(current, oldData) {
			return nil
		}
		if err := b.Put([]byte(k), newData); err != nil {
			return err
		}
		swapped = true
		return nil
	})
	return swapped, err
}

// Increment atomically adds delta to the integer value for k.
// A missing key is treated as zero.
func (s Store) Increment(k string, delta int64) (value int64, err error) {
	err = s.Update(k, &value, func(found bool) error {
		value += delta
		return nil
	})
	return value, err
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
	err = s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(s.bucketName))
		txData := b.Get([]byte(k))
		// txData is only valid during the transaction.
		// Its value must be copied to make it valid outside of the tx.
		// TODO: Benchmark if it's faster to copy + close tx,
		// or to keep the tx open until unmarshalling is done.
		if txData != nil {
			// `data = append([]byte{}, txData...)` would also work, but the following is more explicit
			data = make([]byte, len(txData))
			copy(data, txData)
		}
		return nil
	})
	if err != nil {
		return false, nil
	}

	// If no value was found return false
	if data == nil {
		return false, nil
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

	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(s.bucketName))
		return b.Delete([]byte(k))
	})
}

// Close closes the store.
// It must be called to make sure that all open transactions finish and to release all DB resources.
func (s Store) Close() error {
	return s.db.Close()
}

// Options are the options for the bbolt store.
type Options struct {
	// Bucket name for storing the key-value pairs.
	// Optional ("default" by default).
	BucketName string
	// Path of the DB file.
	// Optional ("bbolt.db" by default).
	Path string
	// Encoding format.
	// Optional (encoding.JSON by default).
	Codec encoding.Codec
}

// DefaultOptions is an Options object with default values.
// BucketName: "default", Path: "bbolt.db", Codec: encoding.JSON
var DefaultOptions = Options{
	BucketName: "default",
	Path:       "bbolt.db",
	Codec:      encoding.JSON,
}

// NewStore creates a new bbolt store.
// Note: bbolt uses an exclusive write lock on the database file so it cannot be shared by multiple processes.
// So when creating multiple clients you should always use a new database file (by setting a different Path in the options).
//
// You must call the Close() method on the store when you're done working with it.
func NewStore(options Options) (Store, error) {
	result := Store{}

	// Set default values
	if options.BucketName == "" {
		options.BucketName = DefaultOptions.BucketName
	}
	if options.Path == "" {
		options.Path = DefaultOptions.Path
	}
	if options.Codec == nil {
		options.Codec = DefaultOptions.Codec
	}

	// Open DB
	db, err := bolt.Open(options.Path, 0600, nil)
	if err != nil {
		return result, err
	}

	// Create a bucket if it doesn't exist yet.
	// In bbolt key/value pairs are stored to and read from buckets.
	err = db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists([]byte(options.BucketName))
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return result, err
	}

	result.db = db
	result.bucketName = options.BucketName
	result.codec = options.Codec

	return result, nil
}

var _ gokv.AtomicUpdater = Store{}
var _ gokv.AtomicComparer = Store{}
var _ gokv.AtomicIncrementer = Store{}
