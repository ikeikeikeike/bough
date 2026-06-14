package config

import "bytes"

// newByteReader wraps a []byte in a bytes.Reader so we can stream it into a
// yaml.NewDecoder without forcing callers to depend on bytes themselves.
func newByteReader(b []byte) *bytes.Reader { return bytes.NewReader(b) }
