package sql

import (
	"database/sql"
	"strings"

	"github.com/philippgille/gokv"
	"github.com/philippgille/gokv/encoding"
	"github.com/philippgille/gokv/util"
)

// Client is a gokv.Store implementation for SQL databases.
type Client struct {
	C          *sql.DB
	UpsertStmt *sql.Stmt
	GetStmt    *sql.Stmt
	DeleteStmt *sql.Stmt
	// IterateStmt must select the key and value columns of all key-value pairs,
	// for example "SELECT k, v FROM Item".
	IterateStmt *sql.Stmt
	Codec       encoding.Codec
}

// Set stores the given value for the given key.
// Values are automatically marshalled to JSON or gob (depending on the configuration).
// The key must not be "" and the value must not be nil.
func (c Client) Set(k string, v any) error {
	if err := util.CheckKeyAndValue(k, v); err != nil {
		return err
	}

	// First turn the passed object into something that the SQL database can handle
	data, err := c.Codec.Marshal(v)
	if err != nil {
		return err
	}

	_, err = c.UpsertStmt.Exec(k, data)
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

	// TODO: Consider using RawBytes.
	dataPtr := new([]byte)
	err = c.GetStmt.QueryRow(k).Scan(dataPtr)
	// If no value was found return false
	if err == sql.ErrNoRows {
		return false, nil
	} else if err != nil {
		return false, err
	}
	data := *dataPtr

	return true, c.Codec.Unmarshal(data, v)
}

// Delete deletes the stored value for the given key.
// Deleting a non-existing key-value pair does NOT lead to an error.
// The key must not be "".
func (c Client) Delete(k string) error {
	if err := util.CheckKey(k); err != nil {
		return err
	}

	_, err := c.DeleteStmt.Exec(k)
	return err
}

// Close closes the client.
// It must be called to return all open connections to the connection pool and to release any open resources.
func (c Client) Close() error {
	return c.C.Close()
}

// Iterate returns an iterator for the key-value pairs whose key starts with the given key prefix.
// An empty prefix iterates over all key-value pairs in the store.
// The iterator streams the rows of a single SELECT query,
// so you can process them without loading all key-value pairs into memory at once.
// Entries that are created or deleted while the iteration is in progress may or may not be returned,
// but entries are never returned multiple times and entries that existed before the iteration are never skipped.
// The store is not locked while you work with the iterator.
func (c Client) Iterate(keyPrefix string) (gokv.Iterator, error) {
	rows, err := c.IterateStmt.Query()
	if err != nil {
		return nil, err
	}

	return &iterator{
		rows:      rows,
		keyPrefix: keyPrefix,
		codec:     c.Codec,
	}, nil
}

type iterator struct {
	rows      *sql.Rows
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
	return i.rows.Close()
}

// CreateDB creates a database with the given name.
// Note 1: When the DataSourceName already contained a database name
// but it doesn't exist yet (error 1049 occurred during Ping()),
// the same error will occur when trying to create the database.
// So this method is only useful when the DataSourceName did NOT contain a database name.
// Note 2: Prepared statements cannot be used for creating and using databases,
// so you must make sure that dbName doesn't contain SQL injections.
func CreateDB(db *sql.DB, dbName string) error {
	_, err := db.Exec("CREATE DATABASE IF NOT EXISTS " + dbName)
	if err != nil {
		return err
	}
	return nil
}
