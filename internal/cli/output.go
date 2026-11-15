package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
)

// Hash is the base of a document: "sha256:" and the hex SHA-256 of its
// bytes.
func Hash(data []byte) string {
	s := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(s[:])
}

// WriteJSON writes one JSON document and a newline. b is the format's
// compact layout; pretty re-indents it.
func WriteJSON(w io.Writer, b []byte, pretty bool) error {
	b = bytes.TrimRight(b, "\n")
	if pretty {
		var out bytes.Buffer
		if err := json.Indent(&out, b, "", "  "); err != nil {
			return err
		}
		b = out.Bytes()
	}
	if _, err := w.Write(append(b, '\n')); err != nil {
		return Errorf(CodeIO, "", "write the result: %v", err)
	}
	return nil
}

// Marshal is the JSON of v without HTML escaping, for results built from
// Go values.
func Marshal(v any) ([]byte, error) { return marshal(v) }
