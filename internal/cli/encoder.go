package cli

import (
	"bytes"
	"encoding/json"
	"io"
)

// ObjectEncoder streams JSON values to a writer one at a time, each
// marshalled once into a reused scratch buffer and then written to the
// underlying writer, so a long list of values needs no output buffer
// and no value is marshalled twice.
type ObjectEncoder struct {
	w   io.Writer
	enc *json.Encoder
	buf bytes.Buffer
}

// NewObjectEncoder returns an ObjectEncoder writing to w.
func NewObjectEncoder(w io.Writer) *ObjectEncoder {
	e := &ObjectEncoder{w: w}
	e.enc = json.NewEncoder(&e.buf)
	e.enc.SetEscapeHTML(false)
	return e
}

// Encode encodes v as JSON and writes it to the underlying writer
// without the newline json.Encoder adds. It reuses the scratch
// buffer, so nothing of v is kept after the call.
func (e *ObjectEncoder) Encode(v any) error {
	b, err := e.EncodeBuffered(v)
	if err != nil {
		return err
	}
	if _, err := e.w.Write(b); err != nil {
		return err
	}
	return nil
}

// EncodeBuffered encodes v into the reused scratch buffer and returns
// its bytes without the trailing newline json.Encoder adds. The
// returned slice aliases the scratch buffer and is valid only until
// the next Encode or EncodeBuffered call, so the caller writes it
// (trimmed, for a fragment of a larger value) to the writer before
// encoding again.
func (e *ObjectEncoder) EncodeBuffered(v any) ([]byte, error) {
	e.buf.Reset()
	if err := e.enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(e.buf.Bytes(), "\n"), nil
}

// WriteString writes s to the underlying writer verbatim, for the
// separators and punctuation between encoded values.
func (e *ObjectEncoder) WriteString(s string) error {
	if _, err := io.WriteString(e.w, s); err != nil {
		return err
	}
	return nil
}
