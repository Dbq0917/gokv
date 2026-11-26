package file

import (
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/philippgille/gokv"
	"github.com/philippgille/gokv/encoding"
	"github.com/philippgille/gokv/util"
)

var defaultFilenameExtension = "json"

// iterateChunkSize is the number of directory entries that are read from the file system at once
// while iterating over the store.
const iterateChunkSize = 100

// Store is a gokv.Store implementation for storing key-value pairs as files.
type Store struct {
	// For locking the locks map
	// (no two goroutines may create a lock for a filename that doesn't have a lock yet).
	locksLock *sync.Mutex
	// For locking file access.
	fileLocks         map[string]*sync.RWMutex
	filenameExtension string
	directory         string
	codec             encoding.Codec
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

	escapedKey := url.PathEscape(k)

	// Prepare file lock.
	lock := s.prepFileLock(escapedKey)

	filename := escapedKey
	if s.filenameExtension != "" {
		filename += "." + s.filenameExtension
	}
	filePath := filepath.Clean(s.directory + "/" + filename)

	// File lock and file handling.
	lock.Lock()
	defer lock.Unlock()
	return os.WriteFile(filePath, data, 0o600)
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

	escapedKey := url.PathEscape(k)

	// Prepare file lock.
	lock := s.prepFileLock(escapedKey)

	filename := escapedKey
	if s.filenameExtension != "" {
		filename += "." + s.filenameExtension
	}
	filePath := filepath.Clean(s.directory + "/" + filename)

	// File lock and file handling.
	lock.RLock()
	// Deferring the unlocking would lead to the unmarshalling being done during the lock, which is bad for performance.
	data, err := os.ReadFile(filePath)
	lock.RUnlock()
	if err != nil {
		if os.IsNotExist(err) {
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

	escapedKey := url.PathEscape(k)

	// Prepare file lock.
	lock := s.prepFileLock(escapedKey)

	filename := escapedKey
	if s.filenameExtension != "" {
		filename += "." + s.filenameExtension
	}
	filePath := filepath.Clean(s.directory + "/" + filename)

	// File lock and file handling.
	lock.Lock()
	defer lock.Unlock()
	err := os.Remove(filePath)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// Close closes the store.
// When called, some resources of the store are left for garbage collection.
func (s Store) Close() error {
	s.locksLock.Lock()
	defer s.locksLock.Unlock()
	for k := range s.fileLocks {
		delete(s.fileLocks, k)
	}
	return nil
}

// Iterate returns an iterator for the key-value pairs whose key starts with the given key prefix.
// An empty prefix iterates over all key-value pairs in the store.
// The iterator reads the directory entries chunk by chunk instead of all at once,
// so entries that are created or deleted while the iteration is in progress may or may not be returned,
// but entries are never returned multiple times and entries that existed before the iteration are never skipped.
// The store is not locked while you work with the iterator.
func (s Store) Iterate(keyPrefix string) (gokv.Iterator, error) {
	dir, err := os.Open(s.directory)
	if err != nil {
		return nil, err
	}

	return &iterator{
		store: s,
		dir:   dir,
		// url.PathEscape escapes at least all characters that can't be used in file names,
		// so we can compare the escaped prefix with the file names.
		escapedPrefix: url.PathEscape(keyPrefix),
	}, nil
}

type iterator struct {
	store         Store
	dir           *os.File
	escapedPrefix string
	entries       []os.DirEntry
	key           string
	err           error
}

// Next advances the iterator to the next entry.
func (i *iterator) Next() bool {
	if i.err != nil {
		return false
	}

	for {
		if len(i.entries) == 0 {
			entries, err := i.dir.ReadDir(iterateChunkSize)
			if err == io.EOF {
				return false
			} else if err != nil {
				i.err = err
				return false
			}
			i.entries = entries
		}

		entry := i.entries[0]
		i.entries = i.entries[1:]

		if entry.IsDir() {
			continue
		}
		filename := entry.Name()
		if i.store.filenameExtension != "" {
			if !strings.HasSuffix(filename, "."+i.store.filenameExtension) {
				continue
			}
			filename = strings.TrimSuffix(filename, "."+i.store.filenameExtension)
		}
		if !strings.HasPrefix(filename, i.escapedPrefix) {
			continue
		}
		key, err := url.PathUnescape(filename)
		if err != nil {
			i.err = err
			return false
		}
		i.key = key
		return true
	}
}

// Key returns the key of the entry the iterator currently points to.
func (i *iterator) Key() string {
	return i.key
}

// Value unmarshals the value of the entry the iterator currently points to into v.
func (i *iterator) Value(v any) (found bool, err error) {
	return i.store.Get(i.key, v)
}

// Err returns the error that caused Next to return false, if any.
func (i *iterator) Err() error {
	return i.err
}

// Close closes the directory handle that backs the iterator.
func (i *iterator) Close() error {
	return i.dir.Close()
}

// prepFileLock returns an existing file lock or creates a new one
func (s Store) prepFileLock(escapedKey string) *sync.RWMutex {
	s.locksLock.Lock()
	lock, found := s.fileLocks[escapedKey]
	if !found {
		lock = new(sync.RWMutex)
		s.fileLocks[escapedKey] = lock
	}
	s.locksLock.Unlock()
	return lock
}

// Options are the options for the Go map store.
type Options struct {
	// The directory in which to store files.
	// Can be absolute or relative.
	// Optional ("gokv" by default).
	Directory string
	// Extension of the filename, e.g. "json".
	// This makes it easier for example to open a file with a text editor that supports syntax highlighting.
	// But it can lead to redundant and/or stale data if you switch the Codec back and forth!
	// You also should make sure to change this when changing the Codec,
	// although it doesn't matter for gokv, but it might be confusing when there's a gob file with a ".json" filename extension.
	// Set to "" to disable.
	// Optional ("json" by default).
	FilenameExtension *string
	// Encoding format.
	// Note: When you change this, you should also change the FilenameExtension if it's not empty ("").
	// Optional (encoding.JSON by default).
	Codec encoding.Codec
}

// DefaultOptions is an Options object with default values.
// Directory: "gokv", Codec: encoding.JSON
var DefaultOptions = Options{
	Directory:         "gokv",
	FilenameExtension: &defaultFilenameExtension,
	Codec:             encoding.JSON,
}

// NewStore creates a new Go map store.
//
// You should call the Close() method on the store when you're done working with it.
func NewStore(options Options) (Store, error) {
	result := Store{}

	// Set default options
	if options.Directory == "" {
		options.Directory = DefaultOptions.Directory
	}
	if options.FilenameExtension == nil {
		options.FilenameExtension = DefaultOptions.FilenameExtension
	}
	if options.Codec == nil {
		options.Codec = DefaultOptions.Codec
	}

	err := os.MkdirAll(options.Directory, 0o700)
	if err != nil {
		return result, err
	}

	result.directory = options.Directory
	result.locksLock = new(sync.Mutex)
	result.fileLocks = make(map[string]*sync.RWMutex)
	result.filenameExtension = *options.FilenameExtension
	result.codec = options.Codec

	return result, nil
}
