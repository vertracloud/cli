package templates

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// ManifestFile is the name of the manifest at the root of a template folder.
const ManifestFile = "vertracloud.template.yaml"

// Manifest is a manifest read from disk: the document as JSON-ready values
// (what the API validates) and the YAML tree, kept to find the line of an issue.
type Manifest struct {
	// File is the path of the manifest file, as shown in messages.
	File string
	// Dir is the folder that holds it; source paths are relative to it.
	Dir string
	// Doc is the manifest as map[string]any, []any, string, int, float64, bool or nil.
	Doc  map[string]any
	root *yaml.Node
}

var yamlLine = regexp.MustCompile(`line (\d+)`)

// SyntaxError is a manifest that is not valid YAML (or not a mapping).
type SyntaxError struct {
	File    string
	Line    int
	Message string
}

func (e *SyntaxError) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("%s:%d: %s", e.File, e.Line, e.Message)
	}
	return fmt.Sprintf("%s: %s", e.File, e.Message)
}

// LoadManifest reads the manifest of target, a folder holding vertracloud.template.yaml
// or the manifest file itself.
func LoadManifest(target string) (*Manifest, error) {
	st, err := os.Stat(target)
	if err != nil {
		return nil, fmt.Errorf("template %q: %w", target, err)
	}
	file := target
	if st.IsDir() {
		file = filepath.Join(target, ManifestFile)
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("%s not found in %s", ManifestFile, filepath.Dir(file))
		}
		return nil, err
	}
	return ParseManifest(file, raw)
}

// ParseManifest parses the YAML of a manifest. file is only used in messages and as the base folder.
func ParseManifest(file string, raw []byte) (*Manifest, error) {
	var tree yaml.Node
	if err := yaml.Unmarshal(raw, &tree); err != nil {
		return nil, syntaxFrom(file, err)
	}
	if len(tree.Content) == 0 {
		return nil, &SyntaxError{File: file, Message: "the manifest is empty"}
	}
	root := tree.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, &SyntaxError{File: file, Line: root.Line, Message: "the manifest must be a mapping of fields"}
	}
	var doc any
	if err := root.Decode(&doc); err != nil {
		return nil, syntaxFrom(file, err)
	}
	m, ok := normalize(doc).(map[string]any)
	if !ok {
		return nil, &SyntaxError{File: file, Line: root.Line, Message: "the manifest must be a mapping of fields"}
	}
	return &Manifest{File: file, Dir: filepath.Dir(file), Doc: m, root: root}, nil
}

func syntaxFrom(file string, err error) *SyntaxError {
	msg := strings.TrimPrefix(err.Error(), "yaml: ")
	line := 0
	if match := yamlLine.FindStringSubmatch(msg); match != nil {
		line, _ = strconv.Atoi(match[1])
		msg = strings.TrimSpace(strings.Replace(msg, match[0]+":", "", 1))
	}
	return &SyntaxError{File: file, Line: line, Message: msg}
}

// normalize turns what yaml.v3 decodes into values that marshal to JSON:
// non-string keys become strings and timestamps become text.
func normalize(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, item := range t {
			t[k] = normalize(item)
		}
		return t
	case map[any]any:
		out := make(map[string]any, len(t))
		for k, item := range t {
			out[fmt.Sprint(k)] = normalize(item)
		}
		return out
	case []any:
		for i := range t {
			t[i] = normalize(t[i])
		}
		return t
	case fmt.Stringer:
		return t.String() // time.Time from an unquoted date
	}
	return v
}

var pathToken = regexp.MustCompile(`[^.\[\]]+`)

// Line returns the 1-based line of path in the manifest, the way the API
// writes it ("inputs[0].id", "services.app.ram"). When the path points at
// something that is not there (a required field that is missing) it returns
// the line of the deepest place that exists, which is where it should be added.
// 0 means unknown.
func (m *Manifest) Line(path string) int {
	node := m.root
	if node == nil {
		return 0
	}
	line := node.Line
	for _, token := range pathToken.FindAllString(path, -1) {
		next := child(node, token)
		if next == nil {
			return line
		}
		node = next.value
		line = next.line
	}
	return line
}

type found struct {
	value *yaml.Node
	line  int
}

func child(node *yaml.Node, token string) *found {
	for node.Kind == yaml.AliasNode && node.Alias != nil {
		node = node.Alias
	}
	switch node.Kind {
	case yaml.MappingNode:
		for i := 0; i+1 < len(node.Content); i += 2 {
			if node.Content[i].Value == token {
				return &found{value: node.Content[i+1], line: node.Content[i].Line}
			}
		}
	case yaml.SequenceNode:
		if n, err := strconv.Atoi(token); err == nil && n >= 0 && n < len(node.Content) {
			return &found{value: node.Content[n], line: node.Content[n].Line}
		}
	}
	return nil
}

// CodeServices returns, by service id, the folder of the code of each service
// whose source is of type "code". The folder must stay inside the manifest's
// folder: an absolute path, a ".." that leaves it or a symlink that leaves it
// is refused.
func (m *Manifest) CodeServices() (map[string]string, error) {
	out := map[string]string{}
	services, _ := m.Doc["services"].(map[string]any)
	base, err := filepath.EvalSymlinks(m.Dir)
	if err != nil {
		return nil, err
	}
	for id, raw := range services {
		service, _ := raw.(map[string]any)
		source, _ := service["source"].(map[string]any)
		if source == nil || source["type"] != "code" {
			continue
		}
		rel, _ := source["path"].(string)
		if strings.TrimSpace(rel) == "" {
			rel = "."
		}
		if filepath.IsAbs(rel) || filepath.VolumeName(rel) != "" {
			return nil, fmt.Errorf("services.%s.source.path must be relative to the manifest folder", id)
		}
		dir, err := filepath.EvalSymlinks(filepath.Join(m.Dir, filepath.FromSlash(rel)))
		if err != nil {
			return nil, fmt.Errorf("services.%s.source.path %q: %w", id, rel, err)
		}
		if inside, err := filepath.Rel(base, dir); err != nil || inside == ".." || strings.HasPrefix(inside, ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("services.%s.source.path %q leaves the manifest folder", id, rel)
		}
		st, err := os.Stat(dir)
		if err != nil {
			return nil, err
		}
		if !st.IsDir() {
			return nil, fmt.Errorf("services.%s.source.path %q must be a folder", id, rel)
		}
		out[id] = dir
	}
	return out, nil
}
