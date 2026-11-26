package consul

import (
	"strings"

	"github.com/hashicorp/consul/api"

	"github.com/philippgille/gokv"
	"github.com/philippgille/gokv/encoding"
	"github.com/philippgille/gokv/util"
)

// Client is a gokv.Store implementation for Consul.
type Client struct {
	c      *api.KV
	folder string
	codec  encoding.Codec
}

// Set stores the given value for the given key.
// Values are automatically marshalled to JSON or gob (depending on the configuration).
// The key must not be "" and the value must not be nil.
func (c Client) Set(k string, v any) error {
	if err := util.CheckKeyAndValue(k, v); err != nil {
		return err
	}

	// First turn the passed object into something that Consul can handle
	data, err := c.codec.Marshal(v)
	if err != nil {
		return err
	}

	if c.folder != "" {
		k = c.folder + "/" + k
	}
	kvPair := api.KVPair{
		Key:   k,
		Value: data,
	}
	_, err = c.c.Put(&kvPair, nil)
	if err != nil {
		return err
	}

	return nil
}

// Get retrieves the stored value for the given key.
// You need to pass a pointer to the value, so in case of a struct
// the automatic unmarshalling can populate the fields of the object
// that v points to with the values of the retrieved object's values.
// If no value is found it returns (false, nil).
// The key must not be "" and the pointer must not be nil.
func (c Client) Get(k string, v any) (found bool, err error) {
	if err := util.CheckKeyAndValue(k, v); err != nil {
		return false, err
	}

	if c.folder != "" {
		k = c.folder + "/" + k
	}
	kvPair, _, err := c.c.Get(k, nil)
	if err != nil {
		return false, err
	}
	// If no value was found return false
	if kvPair == nil {
		return false, nil
	}
	data := kvPair.Value

	return true, c.codec.Unmarshal(data, v)
}

// Delete deletes the stored value for the given key.
// Deleting a non-existing key-value pair does NOT lead to an error.
// The key must not be "".
func (c Client) Delete(k string) error {
	if err := util.CheckKey(k); err != nil {
		return err
	}

	if c.folder != "" {
		k = c.folder + "/" + k
	}
	_, err := c.c.Delete(k, nil)
	return err
}

// Close closes the client.
// In the Consul implementation this doesn't have any effect.
func (c Client) Close() error {
	return nil
}

// Iterate returns an iterator for the key-value pairs whose key starts with the given key prefix.
// An empty prefix iterates over all key-value pairs in the store.
// The iterator works on a consistent snapshot of the store
// (a single Consul list request, which doesn't block writers),
// so entries that are created or deleted while the iteration is in progress are not returned,
// and entries are never returned multiple times or skipped.
// Note: Consul doesn't allow streaming key-value pairs one by one,
// so all matching key-value pairs are loaded into memory when Iterate is called.
func (c Client) Iterate(keyPrefix string) (gokv.Iterator, error) {
	listPrefix := keyPrefix
	if c.folder != "" {
		listPrefix = c.folder + "/" + keyPrefix
	}
	kvPairs, _, err := c.c.List(listPrefix, nil)
	if err != nil {
		return nil, err
	}

	entries := make([]keyValuePair, 0, len(kvPairs))
	for _, kvPair := range kvPairs {
		key := kvPair.Key
		if c.folder != "" {
			key = strings.TrimPrefix(key, c.folder+"/")
		}
		entries = append(entries, keyValuePair{
			key:   key,
			value: kvPair.Value,
		})
	}

	return &iterator{
		entries: entries,
		codec:   c.codec,
		idx:     -1,
	}, nil
}

type keyValuePair struct {
	key   string
	value []byte
}

type iterator struct {
	entries []keyValuePair
	codec   encoding.Codec
	idx     int
}

// Next advances the iterator to the next entry.
func (i *iterator) Next() bool {
	i.idx++
	return i.idx < len(i.entries)
}

// Key returns the key of the entry the iterator currently points to.
func (i *iterator) Key() string {
	return i.entries[i.idx].key
}

// Value unmarshals the value of the entry the iterator currently points to into v.
func (i *iterator) Value(v any) (found bool, err error) {
	if err := util.CheckVal(v); err != nil {
		return false, err
	}

	return true, i.codec.Unmarshal(i.entries[i.idx].value, v)
}

// Err always returns nil because the iteration can't fail.
func (i *iterator) Err() error {
	return nil
}

// Close doesn't have any resources to release.
func (i *iterator) Close() error {
	return nil
}

// Options are the options for the Consul client.
type Options struct {
	// URI scheme for the Consul server.
	// Optional ("http" by default).
	Scheme string
	// Address of the Consul server, including port number.
	// Optional ("127.0.0.1:8500" by default).
	Address string
	// Directory under which to store the key-value pairs.
	// The Consul UI calls this "folder".
	// Optional (none by default).
	Folder string
	// Encoding format.
	// Optional (encoding.JSON by default).
	Codec encoding.Codec
}

// DefaultOptions is an Options object with default values.
// Scheme: "http", Address: "127.0.0.1:8500", Folder: none, Codec: encoding.JSON
var DefaultOptions = Options{
	Scheme:  "http",
	Address: "127.0.0.1:8500",
	Codec:   encoding.JSON,
	// No need to define Folder because its zero value is fine
}

// NewClient creates a new Consul client.
func NewClient(options Options) (Client, error) {
	result := Client{}

	// Set default values
	if options.Scheme == "" {
		options.Scheme = DefaultOptions.Scheme
	}
	if options.Address == "" {
		options.Address = DefaultOptions.Address
	}
	if options.Codec == nil {
		options.Codec = DefaultOptions.Codec
	}

	config := api.DefaultConfig()
	config.Scheme = options.Scheme
	config.Address = options.Address
	client, err := api.NewClient(config)
	if err != nil {
		return result, err
	}

	result.c = client.KV()
	result.folder = options.Folder
	result.codec = options.Codec

	return result, nil
}
