package encoding

import (
	"bytes"
	"encoding/gob"
	"fmt"
)

// GobCodec encodes/decodes Go values to/from gob.
// You can use encoding.Gob instead of creating an instance of this struct.
type GobCodec struct{}

// Marshal encodes a Go value to gob.
// The returned bytes are marked with the gob format (see PrependFormat),
// so that the value can later be identified as gob-encoded,
// regardless of the encoding configuration of the store that reads it.
func (c GobCodec) Marshal(v any) ([]byte, error) {
	buffer := new(bytes.Buffer)
	encoder := gob.NewEncoder(buffer)
	err := encoder.Encode(v)
	if err != nil {
		return nil, err
	}
	return PrependFormat(FormatGob, buffer.Bytes()), nil
}

// Unmarshal decodes a gob value into a Go value.
// Data that was written before values were marked with their format
// is decoded as gob as well.
// An error is returned if the data is marked with a different format.
func (c GobCodec) Unmarshal(data []byte, v any) error {
	format, payload, err := SplitFormat(data)
	if err != nil {
		return err
	}
	switch format {
	case FormatUnknown, FormatGob:
	default:
		return fmt.Errorf("the value is encoded as %s, so it can't be decoded as gob", format)
	}
	reader := bytes.NewReader(payload)
	decoder := gob.NewDecoder(reader)
	return decoder.Decode(v)
}
