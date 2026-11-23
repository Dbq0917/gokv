package gokv_test

import (
	"errors"
	"strconv"
	"testing"

	"github.com/philippgille/gokv"
)

// mockStore is an in-memory gokv.Store implementation for testing GetMultiple.
// Keys in errKeys make Get return an error, simulating a backend failure.
type mockStore struct {
	vals    map[string]any
	errKeys map[string]error
}

func (s mockStore) Set(k string, v any) error {
	s.vals[k] = v
	return nil
}

func (s mockStore) Get(k string, v any) (bool, error) {
	if err, ok := s.errKeys[k]; ok {
		return false, err
	}
	val, ok := s.vals[k]
	if !ok {
		return false, nil
	}
	// The tests only use string values.
	*(v.(*string)) = val.(string)
	return true, nil
}

func (s mockStore) Delete(k string) error {
	delete(s.vals, k)
	return nil
}

func (s mockStore) Close() error {
	return nil
}

func newMockStore() mockStore {
	return mockStore{
		vals:    make(map[string]any),
		errKeys: make(map[string]error),
	}
}

// TestGetMultiple tests that a batch get returns the values of all existing
// keys even when one of the keys was deleted, and that the deleted key is
// reported as missing instead of failing the whole call.
func TestGetMultiple(t *testing.T) {
	store := newMockStore()

	// Store 10 key-value pairs
	keys := make([]string, 0, 10)
	for i := 0; i < 10; i++ {
		key := "key-" + strconv.Itoa(i)
		keys = append(keys, key)
		err := store.Set(key, "val-"+strconv.Itoa(i))
		if err != nil {
			t.Fatal(err)
		}
	}

	// Delete one of them
	deletedKey := keys[6]
	err := store.Delete(deletedKey)
	if err != nil {
		t.Fatal(err)
	}

	found, missing, err := gokv.GetMultiple[string](store, keys)
	if err != nil {
		t.Fatal(err)
	}

	// The 9 existing key-value pairs must be returned
	if len(found) != 9 {
		t.Errorf("Expected 9 found values, but got: %d", len(found))
	}
	for i, key := range keys {
		if key == deletedKey {
			continue
		}
		val, ok := found[key]
		if !ok {
			t.Errorf("Expected a value for key %q, but none was returned", key)
			continue
		}
		expected := "val-" + strconv.Itoa(i)
		if val != expected {
			t.Errorf("Expected value %q for key %q, but got: %q", expected, key, val)
		}
	}

	// The deleted key must be reported as missing
	if len(missing) != 1 || missing[0] != deletedKey {
		t.Errorf("Expected missing to contain only %q, but got: %v", deletedKey, missing)
	}
}

// TestGetMultipleBackendError tests that an actual store failure is returned
// as an error and not swallowed by reporting the key as missing.
func TestGetMultipleBackendError(t *testing.T) {
	backendErr := errors.New("backend unreachable")
	store := newMockStore()

	keys := make([]string, 0, 10)
	for i := 0; i < 10; i++ {
		key := "key-" + strconv.Itoa(i)
		keys = append(keys, key)
		err := store.Set(key, "val-"+strconv.Itoa(i))
		if err != nil {
			t.Fatal(err)
		}
	}
	store.errKeys[keys[3]] = backendErr

	found, missing, err := gokv.GetMultiple[string](store, keys)
	if err == nil {
		t.Fatal("Expected an error, but got none")
	}
	if !errors.Is(err, backendErr) {
		t.Errorf("Expected the error to wrap the backend error, but got: %v", err)
	}
	// No partial results and especially no "missing" report on store failure
	if found != nil {
		t.Errorf("Expected no found values on store failure, but got: %v", found)
	}
	if missing != nil {
		t.Errorf("Expected no missing keys on store failure, but got: %v", missing)
	}
}

// TestGetMultipleEmpty tests that passing no keys leads to an empty result
// and no error.
func TestGetMultipleEmpty(t *testing.T) {
	store := newMockStore()

	found, missing, err := gokv.GetMultiple[string](store, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 0 {
		t.Errorf("Expected no found values, but got: %v", found)
	}
	if len(missing) != 0 {
		t.Errorf("Expected no missing keys, but got: %v", missing)
	}
}
