package bbolt

import (
	bolt "go.etcd.io/bbolt"

	"github.com/philippgille/gokv/util"
)

// SetBytes stores the given raw bytes for the given key, bypassing the codec.
// This implements the optional gokv.ByteStore interface and is used during
// migration to copy values verbatim between stores.
// The key must not be "".
func (s Store) SetBytes(k string, data []byte) error {
	if err := util.CheckKey(k); err != nil {
		return err
	}

	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(s.bucketName))
		return b.Put([]byte(k), data)
	})
}

// GetBytes returns the raw bytes stored under the given key, bypassing the codec.
// This implements the optional gokv.ByteStore interface.
// If no value is found it returns (nil, false, nil).
func (s Store) GetBytes(k string) ([]byte, bool, error) {
	if err := util.CheckKey(k); err != nil {
		return nil, false, err
	}

	var data []byte
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(s.bucketName))
		txData := b.Get([]byte(k))
		// txData is only valid during the transaction, so copy it.
		if txData != nil {
			data = make([]byte, len(txData))
			copy(data, txData)
		}
		return nil
	})
	if err != nil {
		return nil, false, err
	}

	if data == nil {
		return nil, false, nil
	}
	return data, true, nil
}

// Keys returns all keys that currently exist in the store.
// This implements the optional gokv.IterStore interface.
// The keys are a snapshot taken at the time of the call.
func (s Store) Keys() ([]string, error) {
	var keys []string
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(s.bucketName))
		return b.ForEach(func(k, _ []byte) error {
			keys = append(keys, string(k))
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	return keys, nil
}
