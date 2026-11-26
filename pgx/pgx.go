package pgx

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/philippgille/gokv"
	"github.com/philippgille/gokv/encoding"
	"github.com/philippgille/gokv/util"
)

// Options are the options for the PostgreSQL client using pgx.
type Options struct {
	// Pool is pgxpool.Pool.
	Pool *pgxpool.Pool
	// TableName is the name of the table where the data is stored.
	TableName string
	// Codec is the encoding codec for (de)serializing the values.
	Codec encoding.Codec
}

var defaultOptions = Options{
	TableName: "Item",
	Codec:     encoding.JSON,
}

// Client is a gokv.Store implementation for PostgreSQL using pgx.
type Client struct {
	pool      *pgxpool.Pool
	codec     encoding.Codec
	tableName string

	// pgx prepares statements for us, so we don't need to do it manually
	// https://github.com/jackc/pgx/issues/791#issuecomment-660486444
	upsertStmt  string
	getStmt     string
	deleteStmt  string
	iterateStmt string
}

// NewClient creates a new PostgreSQL client using pgx.
//
// You must call the Close() method on the client when you're done working with
// it. Alternatively, you can use the Options Pool's Close() method when you're
// done working with the connection pool.
func NewClient(options Options) (*Client, error) {
	if options.Pool == nil {
		return nil, errors.New("the Pool in the options must not be nil")
	}

	if options.TableName == "" {
		options.TableName = defaultOptions.TableName
	}
	if options.Codec == nil {
		options.Codec = defaultOptions.Codec
	}

	client := &Client{
		pool:        options.Pool,
		codec:       options.Codec,
		tableName:   options.TableName,
		upsertStmt:  fmt.Sprintf("INSERT INTO %s (k, v) VALUES ($1, $2) ON CONFLICT (k) DO UPDATE SET v = EXCLUDED.v", options.TableName),
		getStmt:     fmt.Sprintf("SELECT v FROM %s WHERE k=$1", options.TableName),
		deleteStmt:  fmt.Sprintf("DELETE FROM %s WHERE k=$1", options.TableName),
		iterateStmt: fmt.Sprintf("SELECT k, v FROM %s", options.TableName),
	}

	// Create table if it doesn't exist yet
	_, err := client.pool.Exec(context.Background(), `CREATE TABLE IF NOT EXISTS `+client.tableName+` (k TEXT PRIMARY KEY, v BYTEA NOT NULL)`)
	if err != nil {
		_ = client.Close()
		return nil, err
	}

	return client, nil
}

// Set stores the given value for the given key.
func (c *Client) Set(k string, v any) error {
	if err := util.CheckKeyAndValue(k, v); err != nil {
		return err
	}

	data, err := c.codec.Marshal(v)
	if err != nil {
		return err
	}

	_, err = c.pool.Exec(context.Background(), c.upsertStmt, k, data)
	return err
}

// Get retrieves the stored value for the given key.
func (c *Client) Get(k string, v any) (bool, error) {
	if err := util.CheckKeyAndValue(k, v); err != nil {
		return false, err
	}

	var data []byte
	err := c.pool.QueryRow(context.Background(), c.getStmt, k).Scan(&data)
	if err != nil {
		if err == pgx.ErrNoRows {
			return false, nil
		}
		return false, err
	}

	return true, c.codec.Unmarshal(data, v)
}

// Delete deletes the stored value for the given key.
func (c *Client) Delete(k string) error {
	if err := util.CheckKey(k); err != nil {
		return err
	}

	_, err := c.pool.Exec(context.Background(), c.deleteStmt, k)
	return err
}

// Close closes the connection pool.
func (c *Client) Close() error {
	c.pool.Close()
	return nil
}

// Iterate returns an iterator for the key-value pairs whose key starts with the given key prefix.
// An empty prefix iterates over all key-value pairs in the store.
// The iterator streams the rows of a single SELECT query,
// so you can process them without loading all key-value pairs into memory at once.
// Entries that are created or deleted while the iteration is in progress may or may not be returned,
// but entries are never returned multiple times and entries that existed before the iteration are never skipped.
// The store is not locked while you work with the iterator.
func (c *Client) Iterate(keyPrefix string) (gokv.Iterator, error) {
	rows, err := c.pool.Query(context.Background(), c.iterateStmt)
	if err != nil {
		return nil, err
	}

	return &iterator{
		rows:      rows,
		keyPrefix: keyPrefix,
		codec:     c.codec,
	}, nil
}

type iterator struct {
	rows      pgx.Rows
	keyPrefix string
	codec     encoding.Codec
	key       string
	value     []byte
	err       error
}

// Next advances the iterator to the next entry.
func (i *iterator) Next() bool {
	if i.err != nil {
		return false
	}

	for i.rows.Next() {
		var key string
		var value []byte
		if err := i.rows.Scan(&key, &value); err != nil {
			i.err = err
			return false
		}
		if !strings.HasPrefix(key, i.keyPrefix) {
			continue
		}
		i.key = key
		i.value = value
		return true
	}
	i.err = i.rows.Err()
	return false
}

// Key returns the key of the entry the iterator currently points to.
func (i *iterator) Key() string {
	return i.key
}

// Value unmarshals the value of the entry the iterator currently points to into v.
func (i *iterator) Value(v any) (found bool, err error) {
	if err := util.CheckVal(v); err != nil {
		return false, err
	}

	return true, i.codec.Unmarshal(i.value, v)
}

// Err returns the error that caused Next to return false, if any.
func (i *iterator) Err() error {
	return i.err
}

// Close closes the rows that back the iterator.
func (i *iterator) Close() error {
	i.rows.Close()
	return nil
}
