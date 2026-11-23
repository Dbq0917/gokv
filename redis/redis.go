package redis

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/philippgille/gokv"
	"github.com/philippgille/gokv/encoding"
	"github.com/philippgille/gokv/util"
)

var defaultTimeout = 2 * time.Second

const maxAtomicRetries = 10000

// Client is a gokv.Store implementation for Redis.
type Client struct {
	c       *redis.Client
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

	// First turn the passed object into something that Redis can handle
	// (the Set method takes an interface{}, but the Get method only returns a string,
	// so it can be assumed that the interface{} parameter type is only for convenience
	// for a couple of builtin types like int etc.).
	data, err := c.codec.Marshal(v)
	if err != nil {
		return err
	}

	tctx, cancel := context.WithTimeout(context.Background(), c.timeOut)
	defer cancel()

	err = c.c.Set(tctx, k, string(data), 0).Err()
	if err != nil {
		return err
	}
	return nil
}

// Update atomically reads, modifies and writes the value for the given key.
//
// Redis WATCH provides optimistic concurrency control for the single key.
// If another client changes it before EXEC, the transaction fails and Update
// retries with the latest value. The update function may be called more than
// once, so it must only mutate v and have no external side effects.
func (c Client) Update(k string, v any, update gokv.UpdateFunc) error {
	if err := util.CheckKey(k); err != nil {
		return err
	}
	if err := util.CheckPointer(v); err != nil {
		return err
	}
	if update == nil {
		return errors.New("the update function is nil")
	}

	for attempt := 0; attempt < maxAtomicRetries; attempt++ {
		tctx, cancel := context.WithTimeout(context.Background(), c.timeOut)
		err := c.c.Watch(tctx, func(tx *redis.Tx) error {
			dataString, err := tx.Get(tctx, k).Result()
			found := err == nil
			if err != nil && err != redis.Nil {
				return err
			}

			if err := util.ResetPointer(v); err != nil {
				return err
			}
			if found {
				if err := c.codec.Unmarshal([]byte(dataString), v); err != nil {
					return err
				}
			}
			if err := update(found); err != nil {
				return err
			}

			newData, err := c.codec.Marshal(v)
			if err != nil {
				return err
			}
			_, err = tx.TxPipelined(tctx, func(pipe redis.Pipeliner) error {
				pipe.Set(tctx, k, string(newData), 0)
				return nil
			})
			return err
		}, k)
		cancel()

		if err == redis.TxFailedErr {
			continue
		}
		return err
	}
	return fmt.Errorf("%w (key %q)", gokv.ErrAtomicUpdateRetryExhausted, k)
}

// CompareAndSwap atomically replaces the value for k if it matches oldValue.
// A nil oldValue means that k must not exist.
func (c Client) CompareAndSwap(k string, oldValue, newValue any) (swapped bool, err error) {
	if err := util.CheckKeyAndValue(k, newValue); err != nil {
		return false, err
	}
	if oldValue != nil {
		if err := util.CheckVal(oldValue); err != nil {
			return false, err
		}
	}

	var oldData []byte
	if oldValue != nil {
		oldData, err = c.codec.Marshal(oldValue)
		if err != nil {
			return false, err
		}
	}
	newData, err := c.codec.Marshal(newValue)
	if err != nil {
		return false, err
	}

	for attempt := 0; attempt < maxAtomicRetries; attempt++ {
		tctx, cancel := context.WithTimeout(context.Background(), c.timeOut)
		swapped = false
		err = c.c.Watch(tctx, func(tx *redis.Tx) error {
			currentString, err := tx.Get(tctx, k).Result()
			found := err == nil
			if err != nil && err != redis.Nil {
				return err
			}

			if oldValue == nil {
				if found {
					return nil
				}
			} else if !found || !bytes.Equal([]byte(currentString), oldData) {
				return nil
			}

			_, err = tx.TxPipelined(tctx, func(pipe redis.Pipeliner) error {
				pipe.Set(tctx, k, string(newData), 0)
				return nil
			})
			if err == nil {
				swapped = true
			}
			return err
		}, k)
		cancel()

		if err == redis.TxFailedErr {
			continue
		}
		return swapped, err
	}
	return false, fmt.Errorf("%w (key %q)", gokv.ErrAtomicUpdateRetryExhausted, k)
}

// Increment atomically adds delta to the integer value for k.
// A missing key is treated as zero.
func (c Client) Increment(k string, delta int64) (value int64, err error) {
	err = c.Update(k, &value, func(found bool) error {
		value += delta
		return nil
	})
	return value, err
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

	tctx, cancel := context.WithTimeout(context.Background(), c.timeOut)
	defer cancel()

	dataString, err := c.c.Get(tctx, k).Result()
	if err != nil {
		if err == redis.Nil {
			return false, nil
		}
		return false, err
	}

	return true, c.codec.Unmarshal([]byte(dataString), v)
}

// Delete deletes the stored value for the given key.
// Deleting a non-existing key-value pair does NOT lead to an error.
// The key must not be "".
func (c Client) Delete(k string) error {
	if err := util.CheckKey(k); err != nil {
		return err
	}

	tctx, cancel := context.WithTimeout(context.Background(), c.timeOut)
	defer cancel()

	_, err := c.c.Del(tctx, k).Result()
	return err
}

// Close closes the client.
// It must be called to release any open resources.
func (c Client) Close() error {
	return c.c.Close()
}

// Options are the options for the Redis client.
type Options struct {
	// Address of the Redis server, including the port.
	// Optional ("localhost:6379" by default).
	Address string
	// Password for the Redis server.
	// Optional ("" by default).
	Password string
	// DB to use.
	// Optional (0 by default).
	DB int
	// The timeout for operations.
	// Optional (2 * time.Second by default).
	Timeout *time.Duration
	// Encoding format.
	// Optional (encoding.JSON by default).
	Codec encoding.Codec
}

// DefaultOptions is an Options object with default values.
// Address: "localhost:6379", Password: "", DB: 0, Timeout: 2 * time.Second, Codec: encoding.JSON
var DefaultOptions = Options{
	Address: "localhost:6379",
	Timeout: &defaultTimeout,
	Codec:   encoding.JSON,
	// No need to set Password or DB because their Go zero values are fine for that.
}

// NewClient creates a new Redis client.
//
// You must call the Close() method on the client when you're done working with it.
func NewClient(options Options) (Client, error) {
	result := Client{}

	// Set default values
	if options.Address == "" {
		options.Address = DefaultOptions.Address
	}
	if options.Timeout == nil {
		options.Timeout = DefaultOptions.Timeout
	}
	if options.Codec == nil {
		options.Codec = DefaultOptions.Codec
	}

	client := redis.NewClient(&redis.Options{
		Addr:     options.Address,
		Password: options.Password,
		DB:       options.DB,
	})

	tctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := client.Ping(tctx).Err()
	if err != nil {
		return result, err
	}

	result.c = client
	result.timeOut = *options.Timeout
	result.codec = options.Codec

	return result, nil
}

var _ gokv.AtomicUpdater = Client{}
var _ gokv.AtomicComparer = Client{}
var _ gokv.AtomicIncrementer = Client{}
