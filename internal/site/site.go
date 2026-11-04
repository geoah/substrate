// Package site reads a deployer's site directory (SUBSTRATE_SITE_DIR): the
// brand the console's doors show and the files the server serves at /site/.
// A deployment without one has no site and nothing here runs.
package site

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"

	"github.com/geoah/substrate/internal/substrate"
)

// File is the description every site directory holds.
const File = "site.yaml"

// URLPrefix is where the server serves the directory's files. Discovery's
// `site.logo` and `site.landing` are URLs under it.
const URLPrefix = "/site/"

// maxFile bounds site.yaml. A brand and two file names are a few hundred
// bytes; a file far past this is not a site description.
const maxFile = 64 << 10

// Site is a site directory that passed Load: its absolute path and what
// discovery announces about it.
type Site struct {
	dir  string
	info substrate.SiteInfo
}

// document is site.yaml. Its key set is closed: an unknown key is refused,
// so a misspelled `tagline` is a boot failure rather than a brand that
// silently lost its line.
type document struct {
	Name    string `yaml:"name"`
	Tagline string `yaml:"tagline"`
	Logo    string `yaml:"logo"`
	Landing string `yaml:"landing"`
}

// Load reads and validates the site directory at dir. An empty dir is no
// site: Load returns nil and no error, and the server behaves as if the
// feature did not exist. Every refusal names the problem, because the caller
// refuses to boot on it.
func Load(dir string) (*Site, error) {
	if dir == "" {
		return nil, nil
	}
	if !filepath.IsAbs(dir) {
		return nil, fmt.Errorf("%q is not an absolute path", dir)
	}
	dir = filepath.Clean(dir)
	info, err := os.Stat(dir)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", dir)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()

	raw, err := readDescription(root)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", File, err)
	}
	doc, err := decode(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", File, err)
	}
	s := &Site{dir: dir, info: substrate.SiteInfo{Name: strings.TrimSpace(doc.Name), Tagline: strings.TrimSpace(doc.Tagline)}}
	if s.info.Name == "" {
		return nil, fmt.Errorf("%s: name is required", File)
	}
	if err := plainText(s.info.Name); err != nil {
		return nil, fmt.Errorf("%s: name: %w", File, err)
	}
	if err := plainText(s.info.Tagline); err != nil {
		return nil, fmt.Errorf("%s: tagline: %w", File, err)
	}
	if s.info.Logo, err = fileURL(root, doc.Logo); err != nil {
		return nil, fmt.Errorf("%s: logo: %w", File, err)
	}
	if s.info.Landing, err = fileURL(root, doc.Landing); err != nil {
		return nil, fmt.Errorf("%s: landing: %w", File, err)
	}
	return s, nil
}

// Dir is the directory's absolute path.
func (s *Site) Dir() string { return s.dir }

// Info is what discovery announces.
func (s *Site) Info() substrate.SiteInfo { return s.info }

// Open opens the file at the slash-separated path rel inside the directory,
// for serving. It refuses, as fs.ErrNotExist, every path Servable refuses
// and anything that is not a regular file, and it opens through an os.Root,
// so a symlink that leads out of the directory is refused by the kernel walk
// rather than by a string check that a rename could race.
func (s *Site) Open(rel string) (*os.File, fs.FileInfo, error) {
	if !Servable(rel) {
		return nil, nil, fs.ErrNotExist
	}
	root, err := os.OpenRoot(s.dir)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = root.Close() }()
	return openRegular(root, rel)
}

// Servable reports whether rel is a path the server may serve: relative,
// slash-separated, and every segment a non-empty name that does not start
// with a dot. That one rule refuses `..` and `.`, a dotfile or a dot
// directory (a `.git` checked out beside the files among them), an empty
// segment and a trailing slash, so there is no directory listing to ask for.
// site.yaml is servable: it says nothing discovery does not.
func Servable(rel string) bool {
	if rel == "" || strings.ContainsAny(rel, "\\\x00") || !filepath.IsLocal(filepath.FromSlash(rel)) {
		return false
	}
	for _, seg := range strings.Split(rel, "/") {
		if seg == "" || strings.HasPrefix(seg, ".") {
			return false
		}
	}
	return true
}

func readDescription(root *os.Root) ([]byte, error) {
	f, _, err := openRegular(root, File)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	raw, err := io.ReadAll(io.LimitReader(f, maxFile+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > maxFile {
		return nil, fmt.Errorf("larger than %d bytes", maxFile)
	}
	return raw, nil
}

// keys is site.yaml's closed key set, in the order the refusal lists it.
var keys = []string{"name", "tagline", "logo", "landing"}

func decode(raw []byte) (document, error) {
	var doc document
	var node yaml.Node
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	if err := dec.Decode(&node); err != nil {
		if errors.Is(err, io.EOF) {
			// An empty file decodes to nothing; the name check names it.
			return doc, nil
		}
		return doc, err
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return doc, errors.New("holds more than one YAML document")
	}
	// The key check reads the node, so a refusal names the key and its line
	// rather than the Go type the decoder would have filled.
	if len(node.Content) == 1 && node.Content[0].Kind == yaml.MappingNode {
		m := node.Content[0]
		for i := 0; i+1 < len(m.Content); i += 2 {
			if k := m.Content[i]; !slices.Contains(keys, k.Value) {
				return doc, fmt.Errorf("line %d: unknown key %q (the keys are %s)", k.Line, k.Value, strings.Join(keys, ", "))
			}
		}
	}
	if err := node.Decode(&doc); err != nil {
		return doc, err
	}
	return doc, nil
}

// plainText refuses a control character: the name and the tagline are one
// line each, shown as text.
func plainText(s string) error {
	for _, r := range s {
		if unicode.IsControl(r) {
			return fmt.Errorf("contains the control character %U", r)
		}
	}
	return nil
}

// fileURL checks that rel names a servable regular file in the directory and
// returns its URL under URLPrefix; an empty rel is no file and no URL.
func fileURL(root *os.Root, rel string) (string, error) {
	if rel == "" {
		return "", nil
	}
	if !Servable(rel) {
		return "", fmt.Errorf("%q is not a path inside the directory the server serves: name a file by a relative path with no `..`, no empty segment and no segment starting with a dot", rel)
	}
	f, _, err := openRegular(root, rel)
	if err != nil {
		return "", err
	}
	_ = f.Close()
	segs := strings.Split(rel, "/")
	for i, seg := range segs {
		segs[i] = url.PathEscape(seg)
	}
	return URLPrefix + strings.Join(segs, "/"), nil
}

// openRegular opens rel inside root and refuses anything but a regular file.
// os.Root follows a symlink that stays inside the directory and refuses one
// that leaves it.
func openRegular(root *os.Root, rel string) (*os.File, fs.FileInfo, error) {
	f, err := root.Open(filepath.FromSlash(rel))
	if err != nil {
		return nil, nil, err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, nil, err
	}
	if !info.Mode().IsRegular() {
		_ = f.Close()
		return nil, nil, fmt.Errorf("%s is not a regular file", rel)
	}
	return f, info, nil
}
