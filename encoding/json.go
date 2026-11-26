package encoding

import (
	"encoding/json"
	"fmt"
)

// JSONcodec encodes/decodes Go values to/from JSON.
// You can use encoding.JSON instead of creating an instance of this struct.
type JSONcodec struct{}

// Marshal encodes a Go value to JSON.
// The returned bytes are marked with the JSON format (see PrependFormat),
// so that the value can later be identified as JSON-encoded,
// regardless of the encoding configuration of the store that reads it.
func (c JSONcodec) Marshal(v any) ([]byte, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return PrependFormat(FormatJSON, data), nil
}

// Unmarshal decodes a JSON value into a Go value.
// Data that was written before values were marked with their format
// is decoded as JSON as well.
// An error is returned if the data is marked with a different format.
func (c JSONcodec) Unmarshal(data []byte, v any) error {
	format, payload, err := SplitFormat(data)
	if err != nil {
		return err
	}
	switch format {
	case FormatUnknown, FormatJSON:
		return json.Unmarshal(payload, v)
	default:
		return fmt.Errorf("the value is encoded as %s, so it can't be decoded as JSON", format)
	}
}
