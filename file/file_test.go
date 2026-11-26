package file_test

import (
	"bytes"
	"encoding/gob"
	"log"
	"os"
	"path/filepath"
	"testing"

	"github.com/philippgille/gokv"
	"github.com/philippgille/gokv/encoding"
	"github.com/philippgille/gokv/file"
	"github.com/philippgille/gokv/test"
)

// TestStore tests if reading from, writing to and deleting from the store works properly.
// A struct is used as value. See TestTypes() for a test that is simpler but tests all types.
func TestStore(t *testing.T) {
	// Test with JSON
	t.Run("JSON", func(t *testing.T) {
		store, path := createStore(t, encoding.JSON)
		defer cleanUp(store, path)
		test.TestStore(store, t)
	})

	// Test with gob
	t.Run("gob", func(t *testing.T) {
		store, path := createStore(t, encoding.Gob)
		defer cleanUp(store, path)
		test.TestStore(store, t)
	})
}

// TestTypes tests if setting and getting values works with all Go types.
func TestTypes(t *testing.T) {
	// Test with JSON
	t.Run("JSON", func(t *testing.T) {
		store, path := createStore(t, encoding.JSON)
		defer cleanUp(store, path)
		test.TestTypes(store, t)
	})

	// Test with gob
	t.Run("gob", func(t *testing.T) {
		store, path := createStore(t, encoding.Gob)
		defer cleanUp(store, path)
		test.TestTypes(store, t)
	})
}

// TestStoreConcurrent launches a bunch of goroutines that concurrently work with one store.
// The store is Go map with manual locking via sync.RWMutex, so testing this is important.
func TestStoreConcurrent(t *testing.T) {
	store, path := createStore(t, encoding.JSON)
	defer cleanUp(store, path)

	goroutineCount := 1000

	test.TestConcurrentInteractions(t, goroutineCount, store)
}

// TestErrors tests some error cases.
func TestErrors(t *testing.T) {
	// Test empty key
	store, path := createStore(t, encoding.JSON)
	defer cleanUp(store, path)
	err := store.Set("", "bar")
	if err == nil {
		t.Error("Expected an error")
	}
	_, err = store.Get("", new(string))
	if err == nil {
		t.Error("Expected an error")
	}
	err = store.Delete("")
	if err == nil {
		t.Error("Expected an error")
	}
}

// TestNil tests the behaviour when passing nil or pointers to nil values to some methods.
func TestNil(t *testing.T) {
	// Test setting nil

	t.Run("set nil with JSON marshalling", func(t *testing.T) {
		store, path := createStore(t, encoding.JSON)
		defer cleanUp(store, path)
		err := store.Set("foo", nil)
		if err == nil {
			t.Error("Expected an error")
		}
	})

	t.Run("set nil with Gob marshalling", func(t *testing.T) {
		store, path := createStore(t, encoding.Gob)
		defer cleanUp(store, path)
		err := store.Set("foo", nil)
		if err == nil {
			t.Error("Expected an error")
		}
	})

	// Test passing nil or pointer to nil value for retrieval

	createTest := func(codec encoding.Codec) func(t *testing.T) {
		return func(t *testing.T) {
			store, path := createStore(t, codec)
			defer cleanUp(store, path)

			// Prep
			err := store.Set("foo", test.Foo{Bar: "baz"})
			if err != nil {
				t.Error(err)
			}

			_, err = store.Get("foo", nil) // actually nil
			if err == nil {
				t.Error("An error was expected")
			}

			var i any // actually nil
			_, err = store.Get("foo", i)
			if err == nil {
				t.Error("An error was expected")
			}

			var valPtr *test.Foo // nil value
			_, err = store.Get("foo", valPtr)
			if err == nil {
				t.Error("An error was expected")
			}
		}
	}
	t.Run("get with nil / nil value parameter", createTest(encoding.JSON))
	t.Run("get with nil / nil value parameter", createTest(encoding.Gob))
}

// TestCodecSwitch tests the behaviour when a store is opened with a different codec
// than the one that the stored values were written with.
func TestCodecSwitch(t *testing.T) {
	path := generateRandomTempDBpath(t)
	defer func() { _ = os.RemoveAll(path) }()

	// Write a value with the JSON codec
	jsonStore, err := file.NewStore(file.Options{Directory: path, Codec: encoding.JSON})
	if err != nil {
		t.Fatal(err)
	}
	err = jsonStore.Set("foo", test.Foo{Bar: "baz"})
	if err != nil {
		t.Fatal(err)
	}
	err = jsonStore.Close()
	if err != nil {
		t.Fatal(err)
	}

	// Reopen the store with the gob codec.
	// Reading the JSON-encoded value must lead to an error,
	// and NOT to (false, nil), which is reserved for non-existing keys,
	// and NOT to a silently returned zero value.
	gobStore, err := file.NewStore(file.Options{Directory: path, Codec: encoding.Gob})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanUp(gobStore, path)
	found, err := gobStore.Get("foo", new(test.Foo))
	if err == nil {
		t.Error("Expected an error when reading a JSON-encoded value with the gob codec")
	}
	if !found && err == nil {
		t.Error("(false, nil) must only be returned for non-existing keys")
	}

	// Writing and reading with the new codec works
	err = gobStore.Set("bar", test.Foo{Bar: "baz"})
	if err != nil {
		t.Fatal(err)
	}
	found, err = gobStore.Get("bar", new(test.Foo))
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Error("No value was found, but should have been")
	}
}

// TestLegacyData tests that data written by previous gokv versions is still readable:
// The file name had an encoding-specific extension
// and the file content didn't carry a format marker.
func TestLegacyData(t *testing.T) {
	t.Run("JSON", func(t *testing.T) {
		path := generateRandomTempDBpath(t)
		defer func() { _ = os.RemoveAll(path) }()

		// File as written by gokv versions that appended ".json" to the file name
		// and didn't mark the content with its format
		err := os.WriteFile(filepath.Join(path, "foo.json"), []byte(`{"Bar":"baz"}`), 0o600)
		if err != nil {
			t.Fatal(err)
		}

		store, err := file.NewStore(file.Options{Directory: path})
		if err != nil {
			t.Fatal(err)
		}
		defer cleanUp(store, path)

		val := new(test.Foo)
		found, err := store.Get("foo", val)
		if err != nil {
			t.Fatal(err)
		}
		if !found {
			t.Fatal("No value was found, but should have been")
		}
		if val.Bar != "baz" {
			t.Errorf("Expected: %v, but was: %v", "baz", val.Bar)
		}

		// Deleting the key also deletes the legacy file
		err = store.Delete("foo")
		if err != nil {
			t.Fatal(err)
		}
		_, err = os.Stat(filepath.Join(path, "foo.json"))
		if !os.IsNotExist(err) {
			t.Error("The legacy file should have been deleted")
		}
	})

	t.Run("gob", func(t *testing.T) {
		path := generateRandomTempDBpath(t)
		defer func() { _ = os.RemoveAll(path) }()

		// File as written by gokv versions that appended ".gob" to the file name
		// and didn't mark the content with its format
		buffer := new(bytes.Buffer)
		err := gob.NewEncoder(buffer).Encode(test.Foo{Bar: "baz"})
		if err != nil {
			t.Fatal(err)
		}
		err = os.WriteFile(filepath.Join(path, "foo.gob"), buffer.Bytes(), 0o600)
		if err != nil {
			t.Fatal(err)
		}

		store, err := file.NewStore(file.Options{Directory: path, Codec: encoding.Gob})
		if err != nil {
			t.Fatal(err)
		}
		defer cleanUp(store, path)

		val := new(test.Foo)
		found, err := store.Get("foo", val)
		if err != nil {
			t.Fatal(err)
		}
		if !found {
			t.Fatal("No value was found, but should have been")
		}
		if val.Bar != "baz" {
			t.Errorf("Expected: %v, but was: %v", "baz", val.Bar)
		}
	})
}

// TestClose tests if the close method returns any errors.
func TestClose(t *testing.T) {
	store, path := createStore(t, encoding.JSON)
	defer func() { _ = os.RemoveAll(path) }()
	err := store.Close()
	if err != nil {
		t.Error(err)
	}
}

func createStore(t *testing.T, codec encoding.Codec) (file.Store, string) {
	path := generateRandomTempDBpath(t)
	options := file.Options{
		Directory: path,
		Codec:     codec,
	}
	store, err := file.NewStore(options)
	if err != nil {
		t.Fatal(err)
	}
	return store, path
}

func generateRandomTempDBpath(t *testing.T) string {
	path, err := os.MkdirTemp(os.TempDir(), "gokv")
	if err != nil {
		t.Fatalf("Generating random DB path failed: %v", err)
	}
	return path
}

// cleanUp cleans up the store (deletes the files that have been created during a test).
// If an error occurs the test is NOT marked as failed.
func cleanUp(store gokv.Store, path string) {
	err := store.Close()
	if err != nil {
		log.Printf("Error during cleaning up after a test (during closing the store): %v\n", err)
	}
	err = os.RemoveAll(path)
	if err != nil {
		log.Printf("Error during cleaning up after a test (during removing the data directory): %v\n", err)
	}
}
