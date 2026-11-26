package file

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/philippgille/gokv/encoding"
	"github.com/philippgille/gokv/util"
)

var defaultFilenameExtension = "json"

// ErrCorrupt is returned (wrapped) by Get when the file for a key exists
// but its content can't be decoded, for example because a previous write
// was interrupted. Use errors.Is to check for it.
// The key can be repaired by simply calling Set for it again.
var ErrCorrupt = errors.New("gokv/file: stored data is corrupt")

// tempFilePrefix is the prefix of temporary files used during atomic writes.
// Files with this prefix never contain committed data and may be deleted at any time.
const tempFilePrefix = ".gokv-tmp-"

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
	return writeFileAtomic(s.directory, filePath, data)
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

	err = s.codec.Unmarshal(data, v)
	if err != nil {
		// The file exists but its content is undecodable, e.g. a truncated
		// leftover from a crashed write by an older version. Report it as
		// corruption instead of leaking a raw codec error. A regular Set
		// for the same key atomically replaces the file and repairs the key.
		return true, fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	return true, nil
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

	// Remove temporary files left behind by interrupted atomic writes.
	// They never contain committed data.
	entries, err := os.ReadDir(options.Directory)
	if err != nil {
		return result, err
	}
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), tempFilePrefix) {
			// Best-effort: a file that can't be removed doesn't prevent the store from working.
			_ = os.Remove(filepath.Join(options.Directory, entry.Name()))
		}
	}

	result.directory = options.Directory
	result.locksLock = new(sync.Mutex)
	result.fileLocks = make(map[string]*sync.RWMutex)
	result.filenameExtension = *options.FilenameExtension
	result.codec = options.Codec

	return result, nil
}

// writeFileAtomic writes data to filePath atomically:
// readers either see the complete new content or none of it.
// It does so by writing to a temporary file in the same directory,
// syncing it to disk and then renaming it over the target path.
func writeFileAtomic(dir, filePath string, data []byte) error {
	tmpFile, err := os.CreateTemp(dir, tempFilePrefix+"*")
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()

	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	// Flush the content to disk before the rename, so that after a crash
	// the renamed file doesn't exist with missing content.
	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, filePath); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}

	// Best-effort: sync the directory so the rename itself is durable.
	// Errors are ignored because some platforms/filesystems don't support it.
	if dirFile, err := os.Open(dir); err == nil {
		_ = dirFile.Sync()
		_ = dirFile.Close()
	}

	return nil
}
