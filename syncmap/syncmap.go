package syncmap

import (
	"strings"
	"sync"

	"github.com/philippgille/gokv"
	"github.com/philippgille/gokv/encoding"
	"github.com/philippgille/gokv/util"
)

// Store is a gokv.Store implementation for a Go sync.Map.
type Store struct {
	m     *sync.Map
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

	s.m.Store(k, data)
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

	dataInterface, found := s.m.Load(k)
	if !found {
		return false, nil
	}
	// No need to check "ok" return value in type assertion,
	// because we control the map and we only put slices of bytes in the map.
	data := dataInterface.([]byte)

	return true, s.codec.Unmarshal(data, v)
}

// Delete deletes the stored value for the given key.
// Deleting a non-existing key-value pair does NOT lead to an error.
// The key must not be "".
func (s Store) Delete(k string) error {
	if err := util.CheckKey(k); err != nil {
		return err
	}

	s.m.Delete(k)
	return nil
}

// Close closes the store.
// When called, the store's pointer to the internal Go map is set to nil,
// leading to the map being free for garbage collection.
func (s Store) Close() error {
	// TODO: Requires pointer receiver. We should change this for *all* store
	// implementations and mark it as breaking change.
	// Iterating and deleting individual keys works for the regular map implementation
	// but not here, because without a lock new entries could be added while iterating.
	// Introducing a lock would work but make the syncmap useless.
	// s.m = nil
	return nil
}

// Iterate returns an iterator for the key-value pairs whose key starts with the given key prefix.
// An empty prefix iterates over all key-value pairs in the store.
// The iterator streams the entries one by one via sync.Map's Range method,
// so entries that are created or deleted while the iteration is in progress may or may not be returned,
// but entries are never returned multiple times and entries that existed before the iteration are never skipped.
// The store is not locked while you work with the iterator.
func (s Store) Iterate(keyPrefix string) (gokv.Iterator, error) {
	i := &iterator{
		store: s,
		keys:  make(chan string),
		done:  make(chan struct{}),
	}

	// sync.Map doesn't provide a snapshot or cursor,
	// so we stream the keys from its Range method through a channel.
	go func() {
		defer close(i.keys)
		s.m.Range(func(k, _ any) bool {
			key := k.(string)
			if !strings.HasPrefix(key, keyPrefix) {
				return true
			}
			select {
			case i.keys <- key:
				return true
			case <-i.done:
				return false
			}
		})
	}()

	return i, nil
}

type iterator struct {
	store Store
	keys  chan string
	done  chan struct{}
	key   string
	ok    bool
}

// Next advances the iterator to the next entry.
func (i *iterator) Next() bool {
	i.key, i.ok = <-i.keys
	return i.ok
}

// Key returns the key of the entry the iterator currently points to.
func (i *iterator) Key() string {
	return i.key
}

// Value unmarshals the value of the entry the iterator currently points to into v.
func (i *iterator) Value(v any) (found bool, err error) {
	return i.store.Get(i.key, v)
}

// Err always returns nil because the iteration can't fail.
func (i *iterator) Err() error {
	return nil
}

// Close stops the goroutine that streams the keys, in case the iteration isn't finished yet.
func (i *iterator) Close() error {
	select {
	case <-i.done:
	default:
		close(i.done)
	}
	return nil
}

// Options are the options for the Go sync.Map store.
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

// NewStore creates a new Go sync.Map store.
//
// You should call the Close() method on the store when you're done working with it.
func NewStore(options Options) Store {
	// Set default values
	if options.Codec == nil {
		options.Codec = DefaultOptions.Codec
	}

	return Store{
		m:     &sync.Map{},
		codec: options.Codec,
	}
}
