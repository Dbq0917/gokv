package gomap

import "github.com/philippgille/gokv/util"

// SetBytes stores the given raw bytes for the given key, bypassing the codec.
// This implements the optional gokv.ByteStore interface and is used during
// migration to copy values verbatim between stores.
// The key must not be "".
func (s Store) SetBytes(k string, data []byte) error {
	if err := util.CheckKey(k); err != nil {
		return err
	}

	// Copy the caller's slice so later mutations by the caller don't leak in.
	stored := make([]byte, len(data))
	copy(stored, data)

	s.lock.Lock()
	defer s.lock.Unlock()
	s.m[k] = stored
	return nil
}

// GetBytes returns the raw bytes stored under the given key, bypassing the codec.
// This implements the optional gokv.ByteStore interface.
// If no value is found it returns (nil, false, nil).
func (s Store) GetBytes(k string) ([]byte, bool, error) {
	if err := util.CheckKey(k); err != nil {
		return nil, false, err
	}

	s.lock.RLock()
	data, found := s.m[k]
	s.lock.RUnlock()
	if !found {
		return nil, false, nil
	}

	// Return a copy so the caller can't mutate the internally stored bytes.
	result := make([]byte, len(data))
	copy(result, data)
	return result, true, nil
}

// Keys returns all keys that currently exist in the store.
// This implements the optional gokv.IterStore interface.
// The keys are a snapshot taken at the time of the call.
func (s Store) Keys() ([]string, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	keys := make([]string, 0, len(s.m))
	for k := range s.m {
		keys = append(keys, k)
	}
	return keys, nil
}
