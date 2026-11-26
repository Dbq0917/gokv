package noop

import (
	"github.com/philippgille/gokv"
	"github.com/philippgille/gokv/util"
)

// Store is a gokv.Store implementation that does nothing except validate the arguments if applicable.
type Store struct{}

// Set pretends if stores the key. Always return nil error unless the key or value are invalid.
func (s Store) Set(k string, v any) error {
	if err := util.CheckKeyAndValue(k, v); err != nil {
		return err
	}

	return nil
}

// Get pretends it fetches the key. Always return not found and nil error unless the key or value are invalid.
func (s Store) Get(k string, v any) (found bool, err error) {
	if err := util.CheckKeyAndValue(k, v); err != nil {
		return false, err
	}

	return false, nil
}

// Delete pretends it deletes the key. Always return nil error unless the key is invalid.
func (s Store) Delete(k string) error {
	if err := util.CheckKey(k); err != nil {
		return err
	}

	return nil
}

// Close pretends it closes the store. Always return nil error.
func (s Store) Close() error {
	return nil
}

// Iterate pretends it iterates over the store.
// Because the store never stores anything, the returned iterator is always empty.
// Always return nil error unless the key prefix is invalid.
func (s Store) Iterate(keyPrefix string) (gokv.Iterator, error) {
	return iterator{}, nil
}

type iterator struct{}

// Next pretends it advances the iterator. Always return false because the store never stores anything.
func (i iterator) Next() bool {
	return false
}

// Key pretends it returns the key of the current entry. Always return "" because the store never stores anything.
func (i iterator) Key() string {
	return ""
}

// Value pretends it unmarshals the value of the current entry. Always return not found and nil error.
func (i iterator) Value(v any) (found bool, err error) {
	return false, nil
}

// Err pretends it returns an iteration error. Always return nil error.
func (i iterator) Err() error {
	return nil
}

// Close pretends it closes the iterator. Always return nil error.
func (i iterator) Close() error {
	return nil
}

// NewStore creates a new noop Store that implements gokv.Store interface.
func NewStore() Store {
	return Store{}
}
