package images

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

// MaxContextBytes is the largest build context the API accepts (100 MB). The
// CLI refuses a bigger one before sending anything. It is a variable only so
// tests can lower it.
var MaxContextBytes int64 = 100 << 20

var (
	// ErrContextTooLarge is returned when the context is over MaxContextBytes.
	ErrContextTooLarge = errors.New("build context is larger than 100 MB")
	// ErrNoDockerfile is returned when the folder has no file named exactly Dockerfile at its root.
	ErrNoDockerfile = errors.New("no file named exactly Dockerfile at the root of the context")
)

// ignoreRule is one line of .dockerignore.
type ignoreRule struct {
	re     *regexp.Regexp
	negate bool
}

// ignoreSet follows Docker's rules: the last matching line wins, a line also
// matches everything under a directory it matches, and "!" re-includes.
type ignoreSet struct {
	rules   []ignoreRule
	negates bool
}

func parseDockerignore(content string) ignoreSet {
	var set ignoreSet
	for _, line := range strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		negate := false
		if strings.HasPrefix(line, "!") {
			negate = true
			line = strings.TrimSpace(line[1:])
		}
		line = strings.TrimPrefix(path.Clean("/"+filepath.ToSlash(line)), "/")
		if line == "" || line == "." {
			continue
		}
		re, err := regexp.Compile(globToRegexp(line))
		if err != nil {
			continue // malformed line: Docker rejects the build; here it is simply ignored
		}
		set.rules = append(set.rules, ignoreRule{re: re, negate: negate})
		set.negates = set.negates || negate
	}
	return set
}

func globToRegexp(pattern string) string {
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(pattern); i++ {
		c := pattern[i]
		switch c {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				i++
				if i+1 < len(pattern) && pattern[i+1] == '/' {
					i++
					b.WriteString("(.*/)?")
				} else {
					b.WriteString(".*")
				}
			} else {
				b.WriteString("[^/]*")
			}
		case '?':
			b.WriteString("[^/]")
		case '\\':
			if i+1 < len(pattern) {
				i++
				b.WriteString(regexp.QuoteMeta(string(pattern[i])))
			}
		case '[':
			end := strings.IndexByte(pattern[i:], ']')
			if end <= 1 {
				b.WriteString(`\[`)
				break
			}
			class := pattern[i+1 : i+end]
			if strings.HasPrefix(class, "!") {
				class = "^" + class[1:]
			}
			b.WriteString("[" + strings.ReplaceAll(class, `\`, `\\`) + "]")
			i += end
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteString("$")
	return b.String()
}

// ignored reports whether rel (slash-separated, relative to the root) is excluded.
func (s ignoreSet) ignored(rel string) bool {
	excluded := false
	for _, r := range s.rules {
		if r.negate != excluded {
			continue // a "!" line only matters while excluded; a plain line only while included
		}
		for p := rel; p != "." && p != ""; p = path.Dir(p) {
			if r.re.MatchString(p) {
				excluded = !r.negate
				break
			}
		}
	}
	return excluded
}

// isEnvFile: .env, .env.local, .env.production... never leave the machine,
// whatever .dockerignore says (their keys are read by the server from the zip
// the panel sends; the CLI simply does not send them).
func isEnvFile(name string) bool { return strings.HasPrefix(name, ".env") }

type countingWriter struct {
	w        io.Writer
	n        int64
	max      int64 // 0: no cap
	err      error
	exceeded bool
}

func (c *countingWriter) Write(p []byte) (int, error) {
	c.n += int64(len(p))
	if c.max > 0 && c.n > c.max {
		c.exceeded = true
		return 0, c.err
	}
	return c.w.Write(p)
}

// PackOptions tunes Pack. The zero value packs everything except .env* files
// and symlinks, with no size cap and no ignore file.
type PackOptions struct {
	// IgnoreFiles are looked up at the root of the folder, in order; the first
	// one that exists is parsed with Docker's rules. Empty: nothing is ignored.
	IgnoreFiles []string
	// RequireDockerfile fails with ErrNoDockerfile when there is no regular
	// file named exactly Dockerfile at the root, and keeps it whatever the
	// ignore file says.
	RequireDockerfile bool
	// AlwaysSkip lists names (a file or a folder, at any depth) that never go
	// into the zip, whatever the ignore file says.
	AlwaysSkip []string
	// MaxBytes stops the zip as soon as it passes this size, with ErrTooLarge.
	// 0 means no cap.
	MaxBytes int64
	// ErrTooLarge is returned when MaxBytes is passed; default ErrContextTooLarge.
	ErrTooLarge error
}

// PackDir zips dir into w for an image build: it honors .dockerignore, always
// keeps the root Dockerfile, never includes .env* files or symlinks, and stops
// with ErrContextTooLarge as soon as the zip passes MaxContextBytes.
func PackDir(dir string, w io.Writer) error {
	return Pack(dir, w, PackOptions{IgnoreFiles: []string{".dockerignore"}, RequireDockerfile: true, MaxBytes: MaxContextBytes})
}

// Pack zips dir into w following o. It never includes .env* files or symlinks.
func Pack(dir string, w io.Writer, o PackOptions) error {
	if o.RequireDockerfile {
		if st, err := os.Lstat(filepath.Join(dir, "Dockerfile")); err != nil || !st.Mode().IsRegular() {
			return ErrNoDockerfile
		}
	}
	tooLarge := o.ErrTooLarge
	if tooLarge == nil {
		tooLarge = ErrContextTooLarge
	}
	var set ignoreSet
	for _, name := range o.IgnoreFiles {
		if raw, err := os.ReadFile(filepath.Join(dir, name)); err == nil {
			set = parseDockerignore(string(raw))
			break
		}
	}
	skip := map[string]bool{}
	for _, name := range o.AlwaysSkip {
		skip[name] = true
	}
	cw := &countingWriter{w: w, max: o.MaxBytes, err: tooLarge}
	zw := zip.NewWriter(cw)
	walkErr := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == dir {
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if isEnvFile(d.Name()) || skip[d.Name()] {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		keep := o.RequireDockerfile && rel == "Dockerfile"
		if d.IsDir() {
			if !keep && !set.negates && set.ignored(rel) {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() || (!keep && set.ignored(rel)) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		header.Name = rel
		header.Method = zip.Deflate
		entry, err := zw.CreateHeader(header)
		if err != nil {
			return err
		}
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(entry, f)
		return err
	})
	if cw.exceeded {
		return tooLarge
	}
	if walkErr != nil {
		return walkErr
	}
	if err := zw.Close(); err != nil {
		if cw.exceeded {
			return tooLarge
		}
		return err
	}
	return nil
}

// ContextSource is the build context ready to upload.
type ContextSource struct {
	Reader io.Reader
	// Name is the multipart filename, always ending in .zip.
	Name string
	// Size is the size in bytes of what will be sent.
	Size int64

	file *os.File
	path string // temp zip to remove on Close; empty when the zip came from the user
}

// Close releases the file and removes the temporary zip, if any.
func (s *ContextSource) Close() error {
	if s.file == nil {
		return nil
	}
	err := s.file.Close()
	s.file = nil
	if s.path != "" {
		_ = os.Remove(s.path)
	}
	return err
}

// OpenContext prepares the --context value: a folder is packed (see PackDir)
// and a .zip is sent as it is (the server filters .env). Over 100 MB it fails
// with ErrContextTooLarge before any upload.
func OpenContext(target string) (*ContextSource, error) {
	st, err := os.Stat(target)
	if err != nil {
		return nil, fmt.Errorf("context %q: %w", target, err)
	}
	if !st.IsDir() {
		if !strings.EqualFold(filepath.Ext(target), ".zip") {
			return nil, fmt.Errorf("context %q must be a folder or a .zip file", target)
		}
		if st.Size() > MaxContextBytes {
			return nil, ErrContextTooLarge
		}
		f, err := os.Open(target)
		if err != nil {
			return nil, err
		}
		return &ContextSource{Reader: f, Name: filepath.Base(target), Size: st.Size(), file: f}, nil
	}
	tmp, err := os.CreateTemp("", "vertra-image-*.zip")
	if err != nil {
		return nil, err
	}
	fail := func(err error) (*ContextSource, error) {
		tmp.Close()
		os.Remove(tmp.Name())
		return nil, err
	}
	if err := PackDir(target, tmp); err != nil {
		return fail(err)
	}
	size, err := tmp.Seek(0, io.SeekCurrent)
	if err != nil {
		return fail(err)
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return fail(err)
	}
	name := "context.zip"
	if abs, err := filepath.Abs(target); err == nil && filepath.Base(abs) != string(filepath.Separator) {
		name = filepath.Base(abs) + ".zip"
	}
	return &ContextSource{Reader: tmp, Name: name, Size: size, file: tmp, path: tmp.Name()}, nil
}
