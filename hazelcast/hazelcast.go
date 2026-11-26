package hazelcast

import (
	"context"
	"fmt"
	"strings"

	hazelcast "github.com/hazelcast/hazelcast-go-client"
	"github.com/hazelcast/hazelcast-go-client/logger"

	"github.com/philippgille/gokv"
	"github.com/philippgille/gokv/encoding"
	"github.com/philippgille/gokv/util"
)

// Client is a gokv.Store implementation for Hazelcast.
type Client struct {
	c *hazelcast.Client
	// This map still works even after a temporary connection loss.
	m     *hazelcast.Map
	codec encoding.Codec
}

// Set stores the given value for the given key.
// Values are automatically marshalled to JSON or gob (depending on the configuration).
// The key must not be "" and the value must not be nil.
func (c Client) Set(k string, v any) error {
	if err := util.CheckKeyAndValue(k, v); err != nil {
		return err
	}

	// First turn the passed object into something that Hazelcast can handle
	data, err := c.codec.Marshal(v)
	if err != nil {
		return err
	}

	err = c.m.Set(context.Background(), k, data)
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

	hazelcastValue, err := c.m.Get(context.Background(), k)
	if err != nil {
		return false, err
	}
	// If no value was found return false
	if hazelcastValue == nil {
		return false, nil
	}
	data, ok := hazelcastValue.([]byte)
	if !ok {
		return false, fmt.Errorf("the returned value for key %v was expected to be a slice of bytes, but was type: %T", k, hazelcastValue)
	}

	return true, c.codec.Unmarshal(data, v)
}

// Delete deletes the stored value for the given key.
// The key must not be longer than 250 bytes (this is a restriction of Hazelcast).
// Deleting a non-existing key-value pair does NOT lead to an error.
// The key must not be "".
func (c Client) Delete(k string) error {
	if err := util.CheckKey(k); err != nil {
		return err
	}

	return c.m.Delete(context.Background(), k)
}

// Close closes the client.
// This must be called to properly shut down connections and services (e.g. HeartBeatService).
func (c Client) Close() error {
	return c.c.Shutdown(context.Background())
}

// Iterate returns an iterator for the key-value pairs whose key starts with the given key prefix.
// An empty prefix iterates over all key-value pairs in the store.
// The iterator works on the key set of the map at the time of the call,
// so entries that are created or deleted while the iteration is in progress may or may not be returned,
// but entries are never returned multiple times and entries that existed before the iteration are never skipped.
// The store is not locked while you work with the iterator.
// Note: Hazelcast doesn't allow streaming the keys of a map one by one,
// so all keys (but not their values) are loaded into memory when Iterate is called.
func (c Client) Iterate(keyPrefix string) (gokv.Iterator, error) {
	keySet, err := c.m.GetKeySet(context.Background())
	if err != nil {
		return nil, err
	}

	keys := make([]string, 0, len(keySet))
	for _, k := range keySet {
		key, ok := k.(string)
		if !ok {
			return nil, fmt.Errorf("a key in the map was expected to be a string, but was type: %T", k)
		}
		if strings.HasPrefix(key, keyPrefix) {
			keys = append(keys, key)
		}
	}

	return &iterator{
		client: c,
		keys:   keys,
		idx:    -1,
	}, nil
}

type iterator struct {
	client Client
	keys   []string
	idx    int
}

// Next advances the iterator to the next entry.
func (i *iterator) Next() bool {
	i.idx++
	return i.idx < len(i.keys)
}

// Key returns the key of the entry the iterator currently points to.
func (i *iterator) Key() string {
	return i.keys[i.idx]
}

// Value unmarshals the value of the entry the iterator currently points to into v.
func (i *iterator) Value(v any) (found bool, err error) {
	return i.client.Get(i.keys[i.idx], v)
}

// Err always returns nil because the iteration can't fail.
func (i *iterator) Err() error {
	return nil
}

// Close doesn't have any resources to release.
func (i *iterator) Close() error {
	return nil
}

// Options are the options for the Hazelcast client.
type Options struct {
	// Address of one Hazelcast server, including port.
	// The client will delegate all operations to the given server.
	// If the server dies, the client will automatically switch to another server in the cluster.
	// Optional ("localhost:5701" by default).
	Address string
	// Name of the Hazelcast distributed map to use.
	// Optional ("gokv" by default).
	MapName string
	// Encoding format.
	// Optional (encoding.JSON by default).
	Codec encoding.Codec
}

// DefaultOptions is an Options object with default values.
// Addresses: "localhost:5701", MapName: "gokv", Codec: encoding.JSON
var DefaultOptions = Options{
	Address: "localhost:5701",
	MapName: "gokv",
	Codec:   encoding.JSON,
}

// NewClient creates a new Hazelcast client.
func NewClient(options Options) (Client, error) {
	result := Client{}

	// Set default values
	if options.Address == "" {
		options.Address = DefaultOptions.Address
	}
	if options.MapName == "" {
		options.MapName = DefaultOptions.MapName
	}
	if options.Codec == nil {
		options.Codec = DefaultOptions.Codec
	}

	config := hazelcast.NewConfig()
	config.Cluster.Network.SetAddresses(options.Address)
	config.Logger.Level = logger.OffLevel
	client, err := hazelcast.StartNewClientWithConfig(context.Background(), config)
	if err != nil {
		return result, err
	}

	hazelcastMap, err := client.GetMap(context.Background(), options.MapName)
	if err != nil {
		return result, err
	}

	result.c = client
	result.m = hazelcastMap
	result.codec = options.Codec

	return result, nil
}
