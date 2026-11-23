package gomap

import (
	"bytes"
	"errors"
	"sync"

	"github.com/philippgille/gokv"
	"github.com/philippgille/gokv/encoding"
	"github.com/philippgille/gokv/util"
)

// Store is a gokv.Store implementation for a Go map with a sync.RWMutex for concurrent access.
type Store struct {
	m     map[string][]byte
	lock  *sync.RWMutex
	codec encoding.Codec
}

// Set stores the given value for the given key.
// Values are automatically marshalled to JSON or gob (depending on the configuration).
// The key must not be "" and the value must not be nil.
func (s Store) Set(k string, v any) error {
	if err := util.CheckKeyAndValue(k, v); err != nil {
		return err
	}

	data, err := s.codec.Marshal(v)
	if err != nil {
		return err
	}

	s.lock.Lock()
	defer s.lock.Unlock()
	s.m[k] = data
	return nil
}

// Update atomically reads, modifies and writes the value for the given key.
//
// The store's lock is held for the complete read-modify-write cycle, so
// concurrent updates can't overwrite each other. The update function is called
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

	s.lock.Lock()
	defer s.lock.Unlock()

	data, found := s.m[k]
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

	data, err := s.codec.Marshal(v)
	if err != nil {
		return err
	}
	s.m[k] = data
	return nil
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

	s.lock.Lock()
	defer s.lock.Unlock()

	current, found := s.m[k]
	if oldValue == nil {
		if found {
			return false, nil
		}
	} else if !found || !bytes.Equal(current, oldData) {
		return false, nil
	}
	s.m[k] = newData
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

	s.lock.RLock()
	data, found := s.m[k]
	// Unlock right after reading instead of with defer(),
	// because following unmarshalling will take some time
	// and we don't want to block writing threads until that's done.
	s.lock.RUnlock()
	if !found {
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

	s.lock.Lock()
	defer s.lock.Unlock()
	delete(s.m, k)
	return nil
}

// Close closes the store.
// When called, the store's internal Go map's entries are deleted.
func (s Store) Close() error {
	s.lock.Lock()
	defer s.lock.Unlock()
	for k := range s.m {
		delete(s.m, k)
	}
	return nil
}

// Options are the options for the Go map store.
type Options struct {
	// Encoding format.
	// Optional (encoding.JSON by default).
	Codec encoding.Codec
}

// DefaultOptions is an Options object with default values.
// Codec: encoding.JSON
var DefaultOptions = Options{
	Codec: encoding.JSON,
}

// NewStore creates a new Go map store.
//
// You should call the Close() method on the store when you're done working with it.
func NewStore(options Options) Store {
	// Set default options
	if options.Codec == nil {
		options.Codec = DefaultOptions.Codec
	}

	return Store{
		m:     make(map[string][]byte),
		lock:  new(sync.RWMutex),
		codec: options.Codec,
	}
}

var _ gokv.AtomicUpdater = Store{}
var _ gokv.AtomicComparer = Store{}
var _ gokv.AtomicIncrementer = Store{}
