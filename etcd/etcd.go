package etcd

import (
	"context"
	"errors"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/philippgille/gokv"
	"github.com/philippgille/gokv/encoding"
	"github.com/philippgille/gokv/util"
)

var defaultTimeout = 200 * time.Millisecond

// Client is a gokv.Store implementation for etcd.
type Client struct {
	c       *clientv3.Client
	timeOut time.Duration
	codec   encoding.Codec
}

// Set stores the given value for the given key.
// Values are automatically marshalled to JSON or gob (depending on the configuration).
// The key must not be "" and the value must not be nil.
func (c Client) Set(k string, v any) error {
	if err := util.CheckKeyAndValue(k, v); err != nil {
		return err
	}

	// First turn the passed object into something that etcd can handle
	data, err := c.codec.Marshal(v)
	if err != nil {
		return err
	}

	ctxWithTimeout, cancel := context.WithTimeout(context.Background(), c.timeOut)
	defer cancel()
	_, err = c.c.Put(ctxWithTimeout, k, string(data))
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

	ctxWithTimeout, cancel := context.WithTimeout(context.Background(), c.timeOut)
	defer cancel()
	getRes, err := c.c.Get(ctxWithTimeout, k)
	if err != nil {
		return false, err
	}
	kvs := getRes.Kvs
	// If no value was found return false
	if len(kvs) == 0 {
		return false, nil
	}
	data := kvs[0].Value

	return true, c.codec.Unmarshal(data, v)
}

// Delete deletes the stored value for the given key.
// Deleting a non-existing key-value pair does NOT lead to an error.
// The key must not be "".
func (c Client) Delete(k string) error {
	if err := util.CheckKey(k); err != nil {
		return err
	}

	ctxWithTimeout, cancel := context.WithTimeout(context.Background(), c.timeOut)
	defer cancel()
	_, err := c.c.Delete(ctxWithTimeout, k)
	return err
}

// Close closes the client.
// It must be called to shut down all connections to the etcd server.
func (c Client) Close() error {
	return c.c.Close()
}

// Iterate returns an iterator for the key-value pairs whose key starts with the given key prefix.
// An empty prefix iterates over all key-value pairs in the store.
// The iterator works on a consistent snapshot of the keys
// (a single etcd range request at a specific revision, which doesn't block writers),
// so entries that are created or deleted while the iteration is in progress are not returned,
// and entries are never returned multiple times or skipped.
// Note: etcd doesn't allow streaming the keys of a range one by one,
// so all keys (but not their values) are loaded into memory when Iterate is called.
func (c Client) Iterate(keyPrefix string) (gokv.Iterator, error) {
	ctxWithTimeout, cancel := context.WithTimeout(context.Background(), c.timeOut)
	defer cancel()
	getRes, err := c.c.Get(ctxWithTimeout, keyPrefix, clientv3.WithPrefix(), clientv3.WithKeysOnly())
	if err != nil {
		return nil, err
	}

	keys := make([]string, 0, len(getRes.Kvs))
	for _, kv := range getRes.Kvs {
		keys = append(keys, string(kv.Key))
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

// Options are the options for the etcd client.
type Options struct {
	// Addresses of the etcd servers in the cluster, including port.
	// Optional ([]string{"localhost:2379"} by default).
	Endpoints []string
	// The timeout for operations.
	// Optional (200 * time.Millisecond by default).
	Timeout *time.Duration
	// Encoding format.
	// Optional (encoding.JSON by default).
	Codec encoding.Codec
}

// DefaultOptions is an Options object with default values.
// Endpoints: []string{"localhost:2379"}, Timeout: 200 * time.Millisecond, Codec: encoding.JSON
var DefaultOptions = Options{
	Endpoints: []string{"localhost:2379"},
	Timeout:   &defaultTimeout,
	Codec:     encoding.JSON,
}

// NewClient creates a new etcd client.
//
// You must call the Close() method on the client when you're done working with it.
func NewClient(options Options) (Client, error) {
	result := Client{}

	// Set default values
	if len(options.Endpoints) == 0 {
		options.Endpoints = DefaultOptions.Endpoints
	}
	if options.Timeout == nil {
		options.Timeout = DefaultOptions.Timeout
	}
	if options.Codec == nil {
		options.Codec = DefaultOptions.Codec
	}

	config := clientv3.Config{
		Endpoints:   options.Endpoints,
		DialTimeout: 2 * time.Second,
		// No need for the DialOptions thanks to the explicit Status check below,
		// and even though it's supported as of etcd client v3.6.1, they might move
		// to the new gRPC `NewClient` at any time, which doesn't support it.
		// DialOptions: []grpc.DialOption{grpc.WithBlock()},
	}

	cli, err := clientv3.New(config)
	if err != nil {
		return result, err
	}

	ctxWithTimeout, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	statusRes, err := cli.Status(ctxWithTimeout, options.Endpoints[0])
	if err != nil {
		return result, err
	} else if statusRes == nil {
		return result, errors.New("the status response from etcd was nil")
	}

	result.c = cli
	result.timeOut = *options.Timeout
	result.codec = options.Codec

	return result, nil
}
