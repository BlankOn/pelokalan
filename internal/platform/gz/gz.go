// Package gz compresses and decompresses the gzip blobs stored in the database (PO snapshots, project files)
package gz

import (
	"bytes"
	"compress/gzip"
	"io"
)

// Compress gzips data
func Compress(data []byte) []byte {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	_, _ = zw.Write(data)
	_ = zw.Close()
	return buf.Bytes()
}

// Decompress reads gzipped data
func Decompress(data []byte) ([]byte, error) {
	zr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	return io.ReadAll(zr)
}
