package encoding_test

import (
	"bytes"
	"encoding/gob"
	"encoding/json"
	"testing"

	"github.com/philippgille/gokv/encoding"
)

type foo struct {
	Bar string
}

// TestRoundTrip tests that values encoded with a codec can be decoded with the same codec.
func TestRoundTrip(t *testing.T) {
	codecs := map[string]encoding.Codec{
		"JSON": encoding.JSON,
		"gob":  encoding.Gob,
	}
	for name, codec := range codecs {
		t.Run(name, func(t *testing.T) {
			expected := foo{Bar: "baz"}
			data, err := codec.Marshal(expected)
			if err != nil {
				t.Fatal(err)
			}
			actual := new(foo)
			err = codec.Unmarshal(data, actual)
			if err != nil {
				t.Fatal(err)
			}
			if *actual != expected {
				t.Errorf("Expected: %v, but was: %v", expected, *actual)
			}
		})
	}
}

// TestLegacyData tests that data written by older gokv versions
// (without a format marker) can still be decoded with the configured codec.
func TestLegacyData(t *testing.T) {
	expected := foo{Bar: "baz"}

	t.Run("JSON", func(t *testing.T) {
		// Data as written by gokv versions without format marker
		data, err := json.Marshal(expected)
		if err != nil {
			t.Fatal(err)
		}
		actual := new(foo)
		err = encoding.JSON.Unmarshal(data, actual)
		if err != nil {
			t.Fatal(err)
		}
		if *actual != expected {
			t.Errorf("Expected: %v, but was: %v", expected, *actual)
		}
	})

	t.Run("gob", func(t *testing.T) {
		// Data as written by gokv versions without format marker
		buffer := new(bytes.Buffer)
		err := gob.NewEncoder(buffer).Encode(expected)
		if err != nil {
			t.Fatal(err)
		}
		actual := new(foo)
		err = encoding.Gob.Unmarshal(buffer.Bytes(), actual)
		if err != nil {
			t.Fatal(err)
		}
		if *actual != expected {
			t.Errorf("Expected: %v, but was: %v", expected, *actual)
		}
	})
}

// TestCodecMismatch tests that decoding data with a different codec
// than it was encoded with leads to an error
// instead of a decoding failure or a silently returned zero value.
func TestCodecMismatch(t *testing.T) {
	t.Run("JSON data with gob codec", func(t *testing.T) {
		data, err := encoding.JSON.Marshal(foo{Bar: "baz"})
		if err != nil {
			t.Fatal(err)
		}
		err = encoding.Gob.Unmarshal(data, new(foo))
		if err == nil {
			t.Error("Expected an error")
		}
	})

	t.Run("gob data with JSON codec", func(t *testing.T) {
		data, err := encoding.Gob.Marshal(foo{Bar: "baz"})
		if err != nil {
			t.Fatal(err)
		}
		err = encoding.JSON.Unmarshal(data, new(foo))
		if err == nil {
			t.Error("Expected an error")
		}
	})
}

// TestSplitFormat tests the splitting of the format marker from the payload.
func TestSplitFormat(t *testing.T) {
	// Unmarked data is returned unmodified
	unmarked := []byte(`{"Bar":"baz"}`)
	format, payload, err := encoding.SplitFormat(unmarked)
	if err != nil {
		t.Fatal(err)
	}
	if format != encoding.FormatUnknown {
		t.Errorf("Expected FormatUnknown, but was: %v", format)
	}
	if !bytes.Equal(payload, unmarked) {
		t.Errorf("Expected: %v, but was: %v", unmarked, payload)
	}

	// Marked data is split into format and payload
	marked := encoding.PrependFormat(encoding.FormatJSON, unmarked)
	format, payload, err = encoding.SplitFormat(marked)
	if err != nil {
		t.Fatal(err)
	}
	if format != encoding.FormatJSON {
		t.Errorf("Expected FormatJSON, but was: %v", format)
	}
	if !bytes.Equal(payload, unmarked) {
		t.Errorf("Expected: %v, but was: %v", unmarked, payload)
	}

	// Invalid markers lead to an error
	for _, data := range [][]byte{
		{0x00},       // Marker magic byte without format byte
		{0x00, 0xff}, // Unknown format
	} {
		_, _, err = encoding.SplitFormat(data)
		if err == nil {
			t.Errorf("Expected an error for data %v", data)
		}
	}
}
