package leveldb

import (
	"bytes"
	"errors"
	"sync"

	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/opt"

	"github.com/philippgille/gokv"
	"github.com/philippgille/gokv/encoding"
	"github.com/philippgille/gokv/util"
)

// Store is a gokv.Store implementation for LevelDB.
type Store struct {
	db         *leveldb.DB
	writeSync  bool
	updateLock *sync.Mutex
	codec      encoding.Codec
}

// Set stores the given value for the given key.
// Values are automatically marshalled to JSON or gob (depending on the configuration).
// The key must not be "" and the value must not be nil.
func (s Store) Set(k string, v any) error {
	if err := util.CheckKeyAndValue(k, v); err != nil {
		return err
	}

	// First turn the passed object into something that LevelDB can handle
	data, err := s.codec.Marshal(v)
	if err != nil {
		return err
	}

	var writeOptions *opt.WriteOptions
	if s.writeSync {
		writeOptions = &opt.WriteOptions{
			Sync: true,
		}
	}
	s.updateLock.Lock()
	defer s.updateLock.Unlock()
	return s.db.Put([]byte(k), data, writeOptions)
}

// Update atomically reads, modifies and writes the value for the given key.
//
// LevelDB has no conditional single-key write, so writes are serialized with
// a mutex for the lifetime of this Store. The update function is called
// exactly once by this implementation.
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

	s.updateLock.Lock()
	defer s.updateLock.Unlock()

	data, err := s.db.Get([]byte(k), nil)
	found := err == nil
	if err != nil && err != leveldb.ErrNotFound {
		return err
	}

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
	return s.db.Put([]byte(k), newData, s.writeOptions())
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

	s.updateLock.Lock()
	defer s.updateLock.Unlock()

	current, err := s.db.Get([]byte(k), nil)
	found := err == nil
	if err != nil && err != leveldb.ErrNotFound {
		return false, err
	}
	if oldValue == nil {
		if found {
			return false, nil
		}
	} else if !found || !bytes.Equal(current, oldData) {
		return false, nil
	}

	if err := s.db.Put([]byte(k), newData, s.writeOptions()); err != nil {
		return false, err
	}
	return true, nil
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

	data, err := s.db.Get([]byte(k), nil)
	if err != nil {
		if err == leveldb.ErrNotFound {
			return false, nil
		}
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

	var writeOptions *opt.WriteOptions
	if s.writeSync {
		writeOptions = &opt.WriteOptions{
			Sync: true,
		}
	}
	s.updateLock.Lock()
	defer s.updateLock.Unlock()
	return s.db.Delete([]byte(k), writeOptions)
}

func (s Store) writeOptions() *opt.WriteOptions {
	if !s.writeSync {
		return nil
	}
	return &opt.WriteOptions{Sync: true}
}

// Close closes the store.
// It must be called to releases any outstanding snapshots,
// abort any in-flight compactions and discard open transactions.
func (s Store) Close() error {
	return s.db.Close()
}

// Options are the options for the LevelDB store.
type Options struct {
	// Path of the DB files.
	// Optional ("leveldb" by default).
	Path string
	// Flag to enable immediate file synchronization on writes.
	// If enabled, writes take longer, but no writes are lost when the system crashes.
	// If disabled, writes go to a cache first and are persisted via snapshots automatically.
	// Set() and Delete() are both writes.
	// Optional (false by default).
	WriteSync bool
	// Encoding format.
	// Optional (encoding.JSON by default).
	Codec encoding.Codec
}

// DefaultOptions is an Options object with default values.
// Path: "leveldb", WriteSync: false, Codec: encoding.JSON
var DefaultOptions = Options{
	Path:  "leveldb",
	Codec: encoding.JSON,
	// No need to set WriteSync because its zero values is fine.
}

// NewStore creates a new LevelDB store.
//
// You must call the Close() method on the store when you're done working with it.
func NewStore(options Options) (Store, error) {
	result := Store{}

	// Set default values
	if options.Path == "" {
		options.Path = DefaultOptions.Path
	}
	if options.Codec == nil {
		options.Codec = DefaultOptions.Codec
	}

	// Open DB
	db, err := leveldb.OpenFile(options.Path, nil)
	if err != nil {
		return result, err
	}

	result.db = db
	result.writeSync = options.WriteSync
	result.updateLock = new(sync.Mutex)
	result.codec = options.Codec

	return result, nil
}

var _ gokv.AtomicUpdater = Store{}
var _ gokv.AtomicComparer = Store{}
var _ gokv.AtomicIncrementer = Store{}
