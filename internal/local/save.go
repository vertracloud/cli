package local

import (
	"io"
	"os"
)

// SaveStream writes r to path (mode 0600) and closes r, returning the
// number of bytes written. Used for app and snapshot downloads, which are
// streamed instead of buffered in memory.
func SaveStream(path string, r io.ReadCloser) (int64, error) {
	defer r.Close()
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return 0, err
	}
	n, err := io.Copy(f, r)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return n, err
}
