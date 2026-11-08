// Package glossary builds a terminology glossary from existing translation files.
package glossary

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
)

// File is one translation file read from an upload
type File struct {
	Name string // base name, e.g. "gtk.gtk-4-24.id.po"
	Data []byte
}

// Limits protect the server against oversized or malicious archives
type Limits struct {
	MaxFiles      int
	MaxFileBytes  int64
	MaxTotalBytes int64
}

// DefaultLimits are used when a zero Limits is given
var DefaultLimits = Limits{MaxFiles: 5000, MaxFileBytes: 20 << 20, MaxTotalBytes: 200 << 20}

// Errors returned by ReadFiles
var (
	ErrUnsupported = errors.New("unsupported file type")
	ErrTooLarge    = errors.New("archive too large")
	ErrNoPOFiles   = errors.New("no PO files found")
)

// ReadFiles returns the PO files contained in an upload. Archives are read in
// memory (tar, tar.gz/tgz, zip); a plain PO file is returned as is. Entries
// that are not PO files are ignored. Nothing is written to disk.
func ReadFiles(name string, data []byte, lim Limits) ([]File, error) {
	if lim.MaxFiles == 0 {
		lim = DefaultLimits
	}
	r := &reader{lim: lim}
	var err error
	switch {
	case bytes.HasPrefix(data, []byte{0x1f, 0x8b}):
		err = r.readGzip(name, data)
	case bytes.HasPrefix(data, []byte("PK\x03\x04")):
		err = r.readZip(data)
	case isTar(data):
		err = r.readTar(bytes.NewReader(data))
	case isPOName(name):
		err = r.add(name, data)
	default:
		return nil, ErrUnsupported
	}
	if err != nil {
		return nil, err
	}
	if len(r.files) == 0 {
		return nil, ErrNoPOFiles
	}
	return r.files, nil
}

type reader struct {
	lim   Limits
	files []File
	total int64
}

func isPOName(name string) bool {
	n := strings.ToLower(name)
	return strings.HasSuffix(n, ".po")
}

func isTar(data []byte) bool {
	return len(data) > 262 && string(data[257:262]) == "ustar"
}

func (r *reader) add(name string, data []byte) error {
	if len(r.files) >= r.lim.MaxFiles {
		return ErrTooLarge
	}
	r.total += int64(len(data))
	if r.total > r.lim.MaxTotalBytes {
		return ErrTooLarge
	}
	r.files = append(r.files, File{Name: path.Base(strings.ReplaceAll(name, "\\", "/")), Data: data})
	return nil
}

func (r *reader) readAll(src io.Reader) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(src, r.lim.MaxFileBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > r.lim.MaxFileBytes {
		return nil, ErrTooLarge
	}
	return b, nil
}

func (r *reader) readGzip(name string, data []byte) error {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("gzip: %w", err)
	}
	defer gz.Close()

	// A gzip stream is either a tarball or a single compressed PO file.
	head := make([]byte, 512)
	n, err := io.ReadFull(gz, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return fmt.Errorf("gzip: %w", err)
	}
	head = head[:n]
	rest := io.MultiReader(bytes.NewReader(head), gz)
	if isTar(head) {
		return r.readTar(rest)
	}
	inner := strings.TrimSuffix(strings.TrimSuffix(name, ".gz"), ".GZ")
	if !isPOName(inner) {
		return ErrUnsupported
	}
	b, err := r.readAll(rest)
	if err != nil {
		return err
	}
	return r.add(inner, b)
}

func (r *reader) readTar(src io.Reader) error {
	tr := tar.NewReader(src)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("tar: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg || !isPOName(hdr.Name) {
			continue
		}
		if hdr.Size > r.lim.MaxFileBytes {
			return ErrTooLarge
		}
		b, err := r.readAll(tr)
		if err != nil {
			return err
		}
		if err := r.add(hdr.Name, b); err != nil {
			return err
		}
	}
}

func (r *reader) readZip(data []byte) error {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return fmt.Errorf("zip: %w", err)
	}
	for _, f := range zr.File {
		if f.FileInfo().IsDir() || !isPOName(f.Name) {
			continue
		}
		if int64(f.UncompressedSize64) > r.lim.MaxFileBytes {
			return ErrTooLarge
		}
		rc, err := f.Open()
		if err != nil {
			return fmt.Errorf("zip: %w", err)
		}
		b, err := r.readAll(rc)
		rc.Close()
		if err != nil {
			return err
		}
		if err := r.add(f.Name, b); err != nil {
			return err
		}
	}
	return nil
}
