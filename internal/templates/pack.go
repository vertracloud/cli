package templates

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/vertracloud/cli/internal/images"
)

// MaxSourceBytes is the largest zip the CLI sends for one service (100 MB). It
// is a variable only so tests can lower it.
var MaxSourceBytes int64 = 100 << 20

// ErrSourceTooLarge is returned when the zip of a service passes MaxSourceBytes.
var ErrSourceTooLarge = errors.New("source is larger than 100 MB")

// Package is the zip of the code of each "code" service of a template.
type Package struct {
	// Readers maps a service id to its zip, ready for TemplateCreateParams.Sources.
	Readers map[string]io.Reader
	// Sizes maps a service id to the size of its zip in bytes.
	Sizes map[string]int64

	files []*os.File
}

// Close releases and removes the temporary zips.
func (p *Package) Close() {
	for _, f := range p.files {
		name := f.Name()
		_ = f.Close()
		_ = os.Remove(name)
	}
	p.files = nil
}

// PackSources zips the folder of each code service. It follows .vertraignore,
// or .dockerignore when there is no .vertraignore, in the folder of the
// service. Files named .env*, the .git and .vertra folders and symlinks never
// go in. Each zip is capped at MaxSourceBytes.
func PackSources(m *Manifest) (*Package, error) {
	dirs, err := m.CodeServices()
	if err != nil {
		return nil, err
	}
	pkg := &Package{Readers: map[string]io.Reader{}, Sizes: map[string]int64{}}
	ids := make([]string, 0, len(dirs))
	for id := range dirs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		tmp, err := os.CreateTemp("", "vertra-template-*.zip")
		if err != nil {
			pkg.Close()
			return nil, err
		}
		pkg.files = append(pkg.files, tmp)
		err = images.Pack(dirs[id], tmp, images.PackOptions{
			IgnoreFiles: []string{".vertraignore", ".dockerignore"},
			AlwaysSkip:  []string{".git", ".vertra"},
			MaxBytes:    MaxSourceBytes,
			ErrTooLarge: ErrSourceTooLarge,
		})
		if err != nil {
			pkg.Close()
			return nil, fmt.Errorf("service %q: %w", id, err)
		}
		size, err := tmp.Seek(0, io.SeekCurrent)
		if err == nil {
			_, err = tmp.Seek(0, io.SeekStart)
		}
		if err != nil {
			pkg.Close()
			return nil, err
		}
		pkg.Readers[id] = tmp
		pkg.Sizes[id] = size
	}
	return pkg, nil
}
