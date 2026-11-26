package tablestore

import (
	"errors"
	"strings"
	"time"

	"github.com/aliyun/aliyun-tablestore-go-sdk/tablestore"

	"github.com/philippgille/gokv"
	"github.com/philippgille/gokv/encoding"
	"github.com/philippgille/gokv/util"
)

var keyAttrName = "k"

// iterateLimit is the maximum number of rows that are read from the server at once
// while iterating over the store.
const iterateLimit = 100

// Client is a gokv.Store implementation for Table Store.
type Client struct {
	c         *tablestore.TableStoreClient
	tableName string
	codec     encoding.Codec
}

// Set stores the given value for the given key.
// Values are automatically marshalled to JSON or gob (depending on the configuration).
// The key must not be "" and the value must not be nil.
func (c Client) Set(k string, v any) error {
	if err := util.CheckKeyAndValue(k, v); err != nil {
		return err
	}

	// First turn the passed object into something that Table Store can handle.
	data, err := c.codec.Marshal(v)
	if err != nil {
		return err
	}

	putRowRequest := tablestore.PutRowRequest{
		PutRowChange: &tablestore.PutRowChange{
			Condition: &tablestore.RowCondition{
				RowExistenceExpectation: tablestore.RowExistenceExpectation_IGNORE,
			},
			Columns: []tablestore.AttributeColumn{{
				ColumnName: "v",
				Value:      data,
			}},
			PrimaryKey: &tablestore.PrimaryKey{
				PrimaryKeys: []*tablestore.PrimaryKeyColumn{{
					ColumnName: keyAttrName,
					Value:      k,
				}},
			},
			TableName: c.tableName,
		},
	}
	_, err = c.c.PutRow(&putRowRequest)
	return err
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

	getRowRequest := tablestore.GetRowRequest{
		SingleRowQueryCriteria: &tablestore.SingleRowQueryCriteria{
			ColumnsToGet: []string{"v"},
			MaxVersion:   1,
			PrimaryKey: &tablestore.PrimaryKey{
				PrimaryKeys: []*tablestore.PrimaryKeyColumn{{
					ColumnName: keyAttrName,
					Value:      k,
				}},
			},
			TableName: c.tableName,
		},
	}
	getRowResponse, err := c.c.GetRow(&getRowRequest)
	if err != nil {
		return false, err
	}
	// Return false if no value was found
	if len(getRowResponse.Columns) == 0 {
		return false, nil
	} else if len(getRowResponse.Columns) > 1 {
		return false, errors.New("the returned GetRowResponse should only contain one column, but it contains more than one")
	}
	attributeColumn := getRowResponse.Columns[0]
	if attributeColumn == nil {
		return true, errors.New("a key-value pair for the key was found, but it didn't contain a value")
	}
	dataIface := attributeColumn.Value
	data, ok := dataIface.([]byte)
	if !ok {
		return false, errors.New("the returned value was expected to be a slice of bytes, but it wasn't")
	}

	return true, c.codec.Unmarshal(data, v)
}

// Delete deletes the stored value for the given key.
// Deleting a non-existing key-value pair does NOT lead to an error.
// The key must not be "".
func (c Client) Delete(k string) error {
	if err := util.CheckKey(k); err != nil {
		return err
	}

	deleteRowRequest := tablestore.DeleteRowRequest{
		DeleteRowChange: &tablestore.DeleteRowChange{
			Condition: &tablestore.RowCondition{
				RowExistenceExpectation: tablestore.RowExistenceExpectation_IGNORE,
			},
			PrimaryKey: &tablestore.PrimaryKey{
				PrimaryKeys: []*tablestore.PrimaryKeyColumn{{
					ColumnName: keyAttrName,
					Value:      k,
				}},
			},
			TableName: c.tableName,
		},
	}
	_, err := c.c.DeleteRow(&deleteRowRequest)

	return err
}

// Close closes the client.
// In the Table Store implementation this doesn't have any effect.
func (c Client) Close() error {
	return nil
}

// Iterate returns an iterator for the key-value pairs whose key starts with the given key prefix.
// An empty prefix iterates over all key-value pairs in the store.
// The iterator streams the rows page by page via Table Store's GetRange operation,
// so you can process them without loading all key-value pairs into memory at once.
// Entries that are created or deleted while the iteration is in progress may or may not be returned,
// but entries are never returned multiple times and entries that existed before the iteration are never skipped.
// The store is not locked while you work with the iterator.
func (c Client) Iterate(keyPrefix string) (gokv.Iterator, error) {
	startPrimaryKeyColumn := &tablestore.PrimaryKeyColumn{
		ColumnName: keyAttrName,
		Value:      keyPrefix,
	}
	if keyPrefix == "" {
		startPrimaryKeyColumn = &tablestore.PrimaryKeyColumn{
			ColumnName:       keyAttrName,
			PrimaryKeyOption: tablestore.MIN,
		}
	}

	return &iterator{
		client: c,
		getRangeRequest: tablestore.GetRangeRequest{
			RangeRowQueryCriteria: &tablestore.RangeRowQueryCriteria{
				TableName:    c.tableName,
				ColumnsToGet: []string{"v"},
				MaxVersion:   1,
				Direction:    tablestore.FORWARD,
				Limit:        iterateLimit,
				StartPrimaryKey: &tablestore.PrimaryKey{
					PrimaryKeys: []*tablestore.PrimaryKeyColumn{startPrimaryKeyColumn},
				},
				EndPrimaryKey: &tablestore.PrimaryKey{
					PrimaryKeys: []*tablestore.PrimaryKeyColumn{{
						ColumnName:       keyAttrName,
						PrimaryKeyOption: tablestore.MAX,
					}},
				},
			},
		},
		keyPrefix: keyPrefix,
		first:     true,
	}, nil
}

type iterator struct {
	client          Client
	getRangeRequest tablestore.GetRangeRequest
	keyPrefix       string
	rows            []*tablestore.Row
	key             string
	value           []byte
	err             error
	first           bool
}

// Next advances the iterator to the next entry.
func (i *iterator) Next() bool {
	if i.err != nil {
		return false
	}

	for {
		if len(i.rows) > 0 {
			row := i.rows[0]
			i.rows = i.rows[1:]
			if row.PrimaryKey == nil || len(row.PrimaryKey.PrimaryKeys) == 0 || len(row.Columns) == 0 {
				continue
			}
			key, ok := row.PrimaryKey.PrimaryKeys[0].Value.(string)
			if !ok {
				i.err = errors.New("the primary key of a row was expected to be a string, but wasn't")
				return false
			}
			if !strings.HasPrefix(key, i.keyPrefix) {
				// The rows are ordered by key, so no row with the prefix will follow.
				return false
			}
			value, ok := row.Columns[0].Value.([]byte)
			if !ok {
				i.err = errors.New("the value of a row was expected to be a slice of bytes, but wasn't")
				return false
			}
			i.key = key
			i.value = value
			return true
		}
		if !i.first && i.getRangeRequest.RangeRowQueryCriteria.StartPrimaryKey == nil {
			return false
		}
		i.first = false
		getRangeResponse, err := i.client.c.GetRange(&i.getRangeRequest)
		if err != nil {
			i.err = err
			return false
		}
		i.rows = getRangeResponse.Rows
		i.getRangeRequest.RangeRowQueryCriteria.StartPrimaryKey = getRangeResponse.NextStartPrimaryKey
	}
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

	return true, i.client.codec.Unmarshal(i.value, v)
}

// Err returns the error that caused Next to return false, if any.
func (i *iterator) Err() error {
	return i.err
}

// Close doesn't have any resources to release.
func (i *iterator) Close() error {
	return nil
}

// Options are the options for the Table Store client.
type Options struct {
	// URL of the endpoint.
	// E.g. "https://mytable.ap-southeast-1.ots.aliyuncs.com".
	EndpointURL string
	// Name of the instance.
	// E.g. "mytable".
	InstanceName string
	// AccessKey ID.
	AccessKeyID string
	// AccessKey secret.
	AccessKeySecret string
	// Name of the table.
	// If the table doesn't exist yet, it's created automatically.
	// Optional ("gokv" by default).
	TableName string
	// Reserved read capacity.
	// 0 works fine, but doesn't *guarantee* any capacity.
	// Optional (0 by default).
	ReservedReadCap int
	// Reserved write capacity.
	// 0 works fine, but doesn't *guarantee* any capacity.
	// Optional (0 by default).
	ReservedWriteCap int
	// Encoding format.
	// Optional (encoding.JSON by default).
	Codec encoding.Codec
}

// DefaultOptions is an Options object with default values.
// TableName: "gokv", ReservedReadCap: 0, ReservedWriteCap: 0, Codec: encoding.JSON.
var DefaultOptions = Options{
	TableName: "gokv",
	Codec:     encoding.JSON,
	// No need to set ReservedReadCap or ReservedWriteCap because their Go zero values are fine.
}

// NewClient creates a new Table Store client.
func NewClient(options Options) (Client, error) {
	result := Client{}

	// Precondition check
	if options.EndpointURL == "" {
		return result, errors.New("the EndpointURL of the passed options is empty")
	} else if options.InstanceName == "" {
		return result, errors.New("the InstanceName of the passed options is empty")
	} else if options.AccessKeyID == "" {
		return result, errors.New("the AccessKeyID of the passed options is empty")
	} else if options.AccessKeySecret == "" {
		return result, errors.New("the AccessKeySecret of the passed options is empty")
	}

	// Set default values
	if options.TableName == "" {
		options.TableName = DefaultOptions.TableName
	}
	if options.Codec == nil {
		options.Codec = DefaultOptions.Codec
	}

	config := tablestore.NewDefaultTableStoreConfig()
	// Connection will stay open for multiple requests.
	// Opening the connection may take 2 seconds,
	// a request may take 1 second.
	config.HTTPTimeout.ConnectionTimeout = 2 * time.Second
	config.HTTPTimeout.RequestTimeout = time.Second
	// Default is 5 seconds and 10 retries, which is way too much
	config.MaxRetryTime = time.Second
	config.RetryTimes = 1
	client := tablestore.NewClientWithConfig(options.EndpointURL, options.InstanceName, options.AccessKeyID, options.AccessKeySecret, "", config)

	// Create table if it doesn't exist.
	// Just try to create directly and ignore the error if it's "OTSObjectAlreadyExist",
	// because this way we always have to make just one request instead of sometimes one and sometimes two
	// (one for checking if the table exists and one for creating the table).
	primaryKeyType := tablestore.PrimaryKeyType_STRING
	createtableRequest := tablestore.CreateTableRequest{
		ReservedThroughput: &tablestore.ReservedThroughput{
			Readcap:  options.ReservedReadCap,
			Writecap: options.ReservedWriteCap,
		},
		TableMeta: &tablestore.TableMeta{
			DefinedColumns: []*tablestore.DefinedColumnSchema{{
				ColumnType: tablestore.DefinedColumn_BINARY,
				Name:       "v",
			}},
			SchemaEntry: []*tablestore.PrimaryKeySchema{{
				Name: &keyAttrName,
				Type: &primaryKeyType,
			}},
			TableName: options.TableName,
		},
		TableOption: &tablestore.TableOption{
			// These are the default values as set by the web interface when creating a table.
			// There they're called "Max Versions" and "Time To Live".
			MaxVersion:  1,
			TimeToAlive: -1,
		},
	}
	_, err := client.CreateTable(&createtableRequest)
	if err != nil && !strings.HasPrefix(err.Error(), "OTSObjectAlreadyExist") {
		return result, err
	}

	result.c = client
	result.tableName = options.TableName
	result.codec = options.Codec

	return result, nil
}
