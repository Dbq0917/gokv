package file

import (
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/philippgille/gokv/encoding"
	"github.com/philippgille/gokv/util"
)

// legacyFilenameExtensions are the filename extensions that previous versions
// of this package appended to the file name to indicate the encoding of the file content.
// The encoding of a stored value is now identified by a marker within the stored bytes,
// making the filename extension redundant as encoding indicator,
// but files with those extensions are still read and deleted,
// so that data written by previous versions remains accessible.
var legacyFilenameExtensions = []string{"json", "gob"}

// Store is a gokv.Store implementation for storing key-value pairs as files.
type Store struct {
	// For locking the locks map
	// (no two goroutines may create a lock for a filename that doesn't have a lock yet).
	locksLock *sync.Mutex
	// For locking file access.
	fileLocks map[string]*sync.RWMutex
	directory string
	codec     encoding.Codec
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

	filePath := filepath.Clean(s.directory + "/" + escapedKey)

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

	filePath := filepath.Clean(s.directory + "/" + escapedKey)

	// File lock and file handling.
	lock.RLock()
	// Deferring the unlocking would lead to the unmarshalling being done during the lock, which is bad for performance.
	data, err := os.ReadFile(filePath)
	if err != nil && os.IsNotExist(err) {
		// The file might have been written by a previous version of this package,
		// which appended an encoding-specific extension to the file name.
		legacyFilePath := s.legacyFilePath(escapedKey)
		if legacyFilePath != "" {
			data, err = os.ReadFile(legacyFilePath)
		}
	}
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

	filePath := filepath.Clean(s.directory + "/" + escapedKey)

	// File lock and file handling.
	lock.Lock()
	defer lock.Unlock()
	err := os.Remove(filePath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	// Also delete files that previous versions of this package created for the key.
	for _, extension := range legacyFilenameExtensions {
		legacyFilePath := filepath.Clean(s.directory + "/" + escapedKey + "." + extension)
		err := os.Remove(legacyFilePath)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
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

// legacyFilePath returns the path of the file that previous versions of this package
// created for the given key, or "" if no such file exists.
// Previous versions appended an encoding-specific extension to the file name.
// If files with multiple legacy extensions exist
// (which can happen when the codec was switched back and forth),
// the most recently modified one is returned.
func (s Store) legacyFilePath(escapedKey string) string {
	var result string
	var modTime time.Time
	for _, extension := range legacyFilenameExtensions {
		filePath := filepath.Clean(s.directory + "/" + escapedKey + "." + extension)
		fileInfo, err := os.Stat(filePath)
		if err != nil {
			continue
		}
		if result == "" || fileInfo.ModTime().After(modTime) {
			result = filePath
			modTime = fileInfo.ModTime()
		}
	}
	return result
}

// Options are the options for the file store.
type Options struct {
	// The directory in which to store files.
	// Can be absolute or relative.
	// Optional ("gokv" by default).
	Directory string
	// Encoding format.
	// The stored bytes are marked with the format,
	// so reading a value with a different codec than it was written with
	// leads to an error instead of a decoding failure or a silently returned zero value.
	// Optional (encoding.JSON by default).
	Codec encoding.Codec
}

// DefaultOptions is an Options object with default values.
// Directory: "gokv", Codec: encoding.JSON
var DefaultOptions = Options{
	Directory: "gokv",
	Codec:     encoding.JSON,
}

// NewStore creates a new file store.
//
// You should call the Close() method on the store when you're done working with it.
func NewStore(options Options) (Store, error) {
	result := Store{}

	// Set default options
	if options.Directory == "" {
		options.Directory = DefaultOptions.Directory
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
	result.codec = options.Codec

	return result, nil
}
