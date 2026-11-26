package encoding

import (
	"errors"
	"fmt"
)

// formatMagic is the first byte of every value that carries a format marker.
// It's 0x00, which can't be the first byte of a JSON document, a gob stream
// or a protobuf message, so values written by older gokv versions
// (which don't carry a format marker) are never mistaken for marked values.
const formatMagic = 0x00

// Format identifies the format that a stored value is encoded in.
type Format byte

// Formats that a stored value can be encoded in.
const (
	// FormatUnknown means that the value carries no format marker.
	// That's the case for all values written by gokv versions
	// that didn't mark values with their format yet.
	FormatUnknown Format = iota
	// FormatJSON is the JSON format.
	FormatJSON
	// FormatGob is the gob format.
	FormatGob
)

// String returns the name of the format.
func (f Format) String() string {
	switch f {
	case FormatJSON:
		return "JSON"
	case FormatGob:
		return "gob"
	default:
		return "unknown"
	}
}

// PrependFormat marks data with the format it's encoded in.
// This makes the data self-describing: When it's read back,
// the mark can be checked with SplitFormat,
// so that reading the value with a different codec than it was written with
// leads to an error instead of a decoding failure or a silently returned zero value.
func PrependFormat(format Format, data []byte) []byte {
	marked := make([]byte, 0, len(data)+2)
	marked = append(marked, formatMagic, byte(format))
	return append(marked, data...)
}

// SplitFormat splits the format marker from data
// and returns the format and the actual payload.
// If data carries no marker, FormatUnknown and the unmodified data are returned.
// An error is returned if data carries an invalid marker.
func SplitFormat(data []byte) (format Format, payload []byte, err error) {
	if len(data) == 0 || data[0] != formatMagic {
		return FormatUnknown, data, nil
	}
	if len(data) < 2 {
		return FormatUnknown, nil, errors.New("invalid format marker: marked data must be at least 2 bytes long")
	}
	format = Format(data[1])
	switch format {
	case FormatJSON, FormatGob:
		return format, data[2:], nil
	default:
		return FormatUnknown, nil, fmt.Errorf("unknown format marker: 0x%02x", data[1])
	}
}
