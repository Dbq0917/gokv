package gomap

import (
	"strings"
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

// Iterate returns an iterator for the key-value pairs whose key starts with the given key prefix.
// An empty prefix iterates over all key-value pairs in the store.
// The iterator works on a snapshot of the keys that is taken when Iterate is called,
// so entries that are created or deleted while the iteration is in progress may or may not be returned,
// but entries are never returned multiple times and entries that existed before the iteration are never skipped.
// The store is not locked while you work with the iterator.
func (s Store) Iterate(keyPrefix string) (gokv.Iterator, error) {
	// A Go map can't be iterated safely while it's modified,
	// so we take a snapshot of the keys under a short read lock
	// instead of locking the store for the whole iteration.
	s.lock.RLock()
	keys := make([]string, 0, len(s.m))
	for k := range s.m {
		if strings.HasPrefix(k, keyPrefix) {
			keys = append(keys, k)
		}
	}
	s.lock.RUnlock()

	return &iterator{
		store: s,
		keys:  keys,
		idx:   -1,
	}, nil
}

type iterator struct {
	store Store
	keys  []string
	idx   int
}

// Next advances the iterator to the next entry.
func (i *iterator) Next() bool {
	i.idx++
	return i.idx < len(i.keys)
}

// Key returns the key of the entry the iterator currently points to.
func (i *iterator) Key() string {
	return i.keys[i.idx]
}

// Value unmarshals the value of the entry the iterator currently points to into v.
func (i *iterator) Value(v any) (found bool, err error) {
	return i.store.Get(i.keys[i.idx], v)
}

// Err always returns nil because the iteration can't fail.
func (i *iterator) Err() error {
	return nil
}

// Close doesn't have any resources to release.
func (i *iterator) Close() error {
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
