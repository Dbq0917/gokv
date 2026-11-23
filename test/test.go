package test

import (
	"math/rand"
	"strconv"
	"sync"
	"testing"

	"github.com/go-test/deep"

	"github.com/philippgille/gokv"
)

// Foo is just some struct for common tests.
type Foo struct {
	Bar        string
	privateBar string
}

type privateFoo struct {
	Bar        string
	privateBar string
}

// TestStore tests if reading from, writing to and deleting from the store works properly.
// A struct is used as value. See TestTypes() for a test that is simpler but tests all types.
func TestStore(store gokv.Store, t *testing.T) {
	key := strconv.FormatInt(rand.Int63(), 10)

	// Initially the key shouldn't exist
	found, err := store.Get(key, new(Foo))
	if err != nil {
		t.Error(err)
	}
	if found {
		t.Error("A value was found, but no value was expected")
	}

	// Deleting a non-existing key-value pair should NOT lead to an error
	err = store.Delete(key)
	if err != nil {
		t.Error(err)
	}

	// Store an object
	val := Foo{
		Bar: "baz",
	}
	err = store.Set(key, val)
	if err != nil {
		t.Error(err)
	}

	// Storing it again should not lead to an error but just overwrite it
	err = store.Set(key, val)
	if err != nil {
		t.Error(err)
	}

	// Retrieve the object
	expected := val
	actualPtr := new(Foo)
	found, err = store.Get(key, actualPtr)
	if err != nil {
		t.Error(err)
	}
	if !found {
		t.Error("No value was found, but should have been")
	}
	actual := *actualPtr
	if actual != expected {
		t.Errorf("Expected: %v, but was: %v", expected, actual)
	}

	// Delete
	err = store.Delete(key)
	if err != nil {
		t.Error(err)
	}
	// Key-value pair shouldn't exist anymore
	found, err = store.Get(key, new(Foo))
	if err != nil {
		t.Error(err)
	}
	if found {
		t.Error("A value was found, but no value was expected")
	}
}

// TestTypes tests if setting and getting values works with all Go types.
func TestTypes(store gokv.Store, t *testing.T) {
	boolVar := true
	// Omit byte
	// Omit error - it's a Go builtin type but marshalling and then unmarshalling doesn't lead to equal objects
	floatVar := 1.2
	intVar := 1
	runeVar := '⚡'
	stringVar := "foo"

	structVar := Foo{
		Bar: "baz",
	}
	structWithPrivateFieldVar := Foo{
		Bar:        "baz",
		privateBar: "privBaz",
	}
	// The differing expected var for structWithPrivateFieldVar
	structWithPrivateFieldExpectedVar := Foo{
		Bar: "baz",
	}
	privateStructVar := privateFoo{
		Bar: "baz",
	}
	privateStructWithPrivateFieldVar := privateFoo{
		Bar:        "baz",
		privateBar: "privBaz",
	}
	// The differing expected var for privateStructWithPrivateFieldVar
	privateStructWithPrivateFieldExpectedVar := privateFoo{
		Bar: "baz",
	}

	sliceOfBool := []bool{true, false}
	sliceOfByte := []byte("foo")
	// Omit slice of float
	sliceOfInt := []int{1, 2}
	// Omit slice of rune
	sliceOfString := []string{"foo", "bar"}

	sliceOfSliceOfString := [][]string{{"foo", "bar"}}

	sliceOfStruct := []Foo{{Bar: "baz"}}
	sliceOfPrivateStruct := []privateFoo{{Bar: "baz"}}

	testVals := []struct {
		subTestName string
		val         any
		expected    any
		testGet     func(*testing.T, gokv.Store, string, any)
	}{
		{"bool", boolVar, boolVar, func(t *testing.T, store gokv.Store, key string, expected any) {
			actualPtr := new(bool)
			found, err := store.Get(key, actualPtr)
			handleGetError(t, err, found)
			actual := *actualPtr
			if actual != expected {
				t.Errorf("Expected: %v, but was: %v", expected, actual)
			}
		}},
		{"float", floatVar, floatVar, func(t *testing.T, store gokv.Store, key string, expected any) {
			actualPtr := new(float64)
			found, err := store.Get(key, actualPtr)
			handleGetError(t, err, found)
			actual := *actualPtr
			if actual != expected {
				t.Errorf("Expected: %v, but was: %v", expected, actual)
			}
		}},
		{"int", intVar, intVar, func(t *testing.T, store gokv.Store, key string, expected any) {
			actualPtr := new(int)
			found, err := store.Get(key, actualPtr)
			handleGetError(t, err, found)
			actual := *actualPtr
			if actual != expected {
				t.Errorf("Expected: %v, but was: %v", expected, actual)
			}
		}},
		{"rune", runeVar, runeVar, func(t *testing.T, store gokv.Store, key string, expected any) {
			actualPtr := new(rune)
			found, err := store.Get(key, actualPtr)
			handleGetError(t, err, found)
			actual := *actualPtr
			if actual != expected {
				t.Errorf("Expected: %v, but was: %v", expected, actual)
			}
		}},
		{"string", stringVar, stringVar, func(t *testing.T, store gokv.Store, key string, expected any) {
			actualPtr := new(string)
			found, err := store.Get(key, actualPtr)
			handleGetError(t, err, found)
			actual := *actualPtr
			if actual != expected {
				t.Errorf("Expected: %v, but was: %v", expected, actual)
			}
		}},
		{"struct", structVar, structVar, func(t *testing.T, store gokv.Store, key string, expected any) {
			actualPtr := new(Foo)
			found, err := store.Get(key, actualPtr)
			handleGetError(t, err, found)
			actual := *actualPtr
			if actual != expected {
				t.Errorf("Expected: %v, but was: %v", expected, actual)
			}
		}},
		{"struct with private field", structWithPrivateFieldVar, structWithPrivateFieldExpectedVar, func(t *testing.T, store gokv.Store, key string, expected any) {
			actualPtr := new(Foo)
			found, err := store.Get(key, actualPtr)
			handleGetError(t, err, found)
			actual := *actualPtr
			if actual != expected {
				t.Errorf("Expected: %v, but was: %v", expected, actual)
			}
		}},
		{"private struct", privateStructVar, privateStructVar, func(t *testing.T, store gokv.Store, key string, expected any) {
			actualPtr := new(privateFoo)
			found, err := store.Get(key, actualPtr)
			handleGetError(t, err, found)
			actual := *actualPtr
			if actual != expected {
				t.Errorf("Expected: %v, but was: %v", expected, actual)
			}
		}},
		{"private struct with private field", privateStructWithPrivateFieldVar, privateStructWithPrivateFieldExpectedVar, func(t *testing.T, store gokv.Store, key string, expected any) {
			actualPtr := new(privateFoo)
			found, err := store.Get(key, actualPtr)
			handleGetError(t, err, found)
			actual := *actualPtr
			if actual != expected {
				t.Errorf("Expected: %v, but was: %v", expected, actual)
			}
		}},
		{"slice of bool", sliceOfBool, sliceOfBool, func(t *testing.T, store gokv.Store, key string, expected any) {
			actualPtr := new([]bool)
			found, err := store.Get(key, actualPtr)
			handleGetError(t, err, found)
			actual := *actualPtr
			if diff := deep.Equal(actual, expected); diff != nil {
				t.Error(diff)
			}
		}},
		{"slice of byte", sliceOfByte, sliceOfByte, func(t *testing.T, store gokv.Store, key string, expected any) {
			actualPtr := new([]byte)
			found, err := store.Get(key, actualPtr)
			handleGetError(t, err, found)
			actual := *actualPtr
			if diff := deep.Equal(actual, expected); diff != nil {
				t.Error(diff)
			}
		}},
		{"slice of int", sliceOfInt, sliceOfInt, func(t *testing.T, store gokv.Store, key string, expected any) {
			actualPtr := new([]int)
			found, err := store.Get(key, actualPtr)
			handleGetError(t, err, found)
			actual := *actualPtr
			if diff := deep.Equal(actual, expected); diff != nil {
				t.Error(diff)
			}
		}},
		{"slice of string", sliceOfString, sliceOfString, func(t *testing.T, store gokv.Store, key string, expected any) {
			actualPtr := new([]string)
			found, err := store.Get(key, actualPtr)
			handleGetError(t, err, found)
			actual := *actualPtr
			if diff := deep.Equal(actual, expected); diff != nil {
				t.Error(diff)
			}
		}},
		{"slice of slice of string", sliceOfSliceOfString, sliceOfSliceOfString, func(t *testing.T, store gokv.Store, key string, expected any) {
			actualPtr := new([][]string)
			found, err := store.Get(key, actualPtr)
			handleGetError(t, err, found)
			actual := *actualPtr
			if diff := deep.Equal(actual, expected); diff != nil {
				t.Error(diff)
			}
		}},
		{"slice of struct", sliceOfStruct, sliceOfStruct, func(t *testing.T, store gokv.Store, key string, expected any) {
			actualPtr := new([]Foo)
			found, err := store.Get(key, actualPtr)
			handleGetError(t, err, found)
			actual := *actualPtr
			if diff := deep.Equal(actual, expected); diff != nil {
				t.Error(diff)
			}
		}},
		{"slice of private struct", sliceOfPrivateStruct, sliceOfPrivateStruct, func(t *testing.T, store gokv.Store, key string, expected any) {
			actualPtr := new([]privateFoo)
			found, err := store.Get(key, actualPtr)
			handleGetError(t, err, found)
			actual := *actualPtr
			if diff := deep.Equal(actual, expected); diff != nil {
				t.Error(diff)
			}
		}},
	}

	for _, testVal := range testVals {
		t.Run(testVal.subTestName, func(t2 *testing.T) {
			key := strconv.FormatInt(rand.Int63(), 10)
			err := store.Set(key, testVal.val)
			if err != nil {
				t.Error(err)
			}
			testVal.testGet(t, store, key, testVal.expected)
		})
	}
}

func handleGetError(t *testing.T, err error, found bool) {
	if err != nil {
		t.Error(err)
	}
	if !found {
		t.Error("No value was found, but should have been")
	}
}

// TestBatchDeleter tests if deleting multiple key-value pairs in a single batch works properly,
// including the atomicity guarantee of the gokv.BatchDeleter interface:
// When the batch fails, no key-value pair of the batch may be deleted.
// The store must implement gokv.BatchDeleter, otherwise the test fails.
func TestBatchDeleter(store gokv.Store, t *testing.T) {
	batchDeleter, ok := store.(gokv.BatchDeleter)
	if !ok {
		t.Fatal("The store doesn't implement gokv.BatchDeleter")
	}

	// Use keys with a common prefix, as batch deletes are typically used
	// to clean up a group of related key-value pairs.
	prefix := "batchdel-" + strconv.FormatInt(rand.Int63(), 10) + "-"
	keyCount := 10
	keys := make([]string, keyCount)
	for i := 0; i < keyCount; i++ {
		keys[i] = prefix + strconv.Itoa(i)
	}
	// A control key outside of the batch, which must never be touched.
	controlKey := prefix + "control"

	setAll := func() {
		for _, key := range keys {
			err := store.Set(key, "bar")
			if err != nil {
				t.Fatal(err)
			}
		}
		err := store.Set(controlKey, "bar")
		if err != nil {
			t.Fatal(err)
		}
	}
	// countRemaining returns the number of the batch keys that still exist in the store.
	countRemaining := func() int {
		remaining := 0
		for _, key := range keys {
			found, err := store.Get(key, new(string))
			if err != nil {
				t.Fatal(err)
			}
			if found {
				remaining++
			}
		}
		return remaining
	}

	// Deleting non-existing key-value pairs in a batch should NOT lead to an error
	err := batchDeleter.DeleteBatch(keys)
	if err != nil {
		t.Error(err)
	}

	// An empty batch should NOT lead to an error
	err = batchDeleter.DeleteBatch([]string{})
	if err != nil {
		t.Error(err)
	}

	// Happy path: A full batch is deleted at once
	setAll()
	err = batchDeleter.DeleteBatch(keys)
	if err != nil {
		t.Error(err)
	}
	if remaining := countRemaining(); remaining != 0 {
		t.Errorf("Expected no key-value pairs to remain after the batch delete, but %v remained", remaining)
	}
	// The control key-value pair must still exist
	found, err := store.Get(controlKey, new(string))
	if err != nil {
		t.Error(err)
	}
	if !found {
		t.Error("The control key-value pair was deleted, but it wasn't part of the batch")
	}

	// Failure case: A batch containing an invalid key must fail
	// and leave ALL key-value pairs of the batch untouched (atomicity).
	setAll()
	invalidBatch := make([]string, 0, len(keys)+1)
	// Put the invalid key in the middle of the batch, so that an implementation
	// that deletes one by one would already have deleted some key-value pairs
	// when encountering it.
	invalidBatch = append(invalidBatch, keys[:keyCount/2]...)
	invalidBatch = append(invalidBatch, "")
	invalidBatch = append(invalidBatch, keys[keyCount/2:]...)
	err = batchDeleter.DeleteBatch(invalidBatch)
	if err == nil {
		t.Error("Expected an error for a batch containing an empty key, but got none")
	}
	if remaining := countRemaining(); remaining != keyCount {
		t.Errorf("Expected all %v key-value pairs to remain after the failed batch delete, but only %v remained", keyCount, remaining)
	}
}

// TestConcurrentInteractions launches a bunch of goroutines that concurrently work with the store.
func TestConcurrentInteractions(t *testing.T, goroutineCount int, store gokv.Store) {
	waitGroup := sync.WaitGroup{}
	waitGroup.Add(goroutineCount) // Must be called before any goroutine is started
	for i := 0; i < goroutineCount; i++ {
		go InteractWithStore(store, strconv.Itoa(i), t, &waitGroup)
	}
	waitGroup.Wait()

	// Now make sure that all values are in the store
	expected := Foo{}
	for i := 0; i < goroutineCount; i++ {
		actualPtr := new(Foo)
		found, err := store.Get(strconv.Itoa(i), actualPtr)
		if err != nil {
			t.Errorf("An error occurred during the test: %v", err)
		}
		if !found {
			t.Error("No value was found, but should have been")
		}
		actual := *actualPtr
		if actual != expected {
			t.Errorf("Expected: %v, but was: %v", expected, actual)
		}
	}
}

// InteractWithStore reads from and writes to the DB. Meant to be executed in a goroutine.
// Does NOT check if the DB works correctly (that's done elsewhere),
// only checks for errors that might occur due to concurrent access.
func InteractWithStore(store gokv.Store, key string, t *testing.T, waitGroup *sync.WaitGroup) {
	defer waitGroup.Done()

	// Read
	_, err := store.Get(key, new(Foo))
	if err != nil {
		t.Error(err)
	}
	// Write
	err = store.Set(key, Foo{})
	if err != nil {
		t.Error(err)
	}
	// Read
	_, err = store.Get(key, new(Foo))
	if err != nil {
		t.Error(err)
	}
	// Delete
	err = store.Delete(key)
	if err != nil {
		t.Error(err)
	}
	// Read
	_, err = store.Get(key, new(Foo))
	if err != nil {
		t.Error(err)
	}
	// Final write so the caller can iterate over all keys sequentially and check if they exist
	err = store.Set(key, Foo{})
	if err != nil {
		t.Error(err)
	}
}
