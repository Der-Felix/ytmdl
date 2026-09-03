package metadata

import (
	"bufio"
	"io"
)

// newTestReader wraps a reader the way the Ogg parser expects it.
func newTestReader(r io.Reader) *bufio.Reader {
	return bufio.NewReaderSize(r, 64*1024)
}
