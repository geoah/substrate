package site

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

func write(t *testing.T, dir, rel, body string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadEmptyDirIsNoSite(t *testing.T) {
	s, err := Load("")
	if err != nil || s != nil {
		t.Fatalf("Load(\"\") = %v, %v; want nil, nil", s, err)
	}
}

func TestLoadReadsEveryKey(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "site.yaml", "name: Example\ntagline: A place for friends\nlogo: img/logo one.svg\nicon: icon.png\nlanding: landing.html\n")
	write(t, dir, "img/logo one.svg", "<svg/>")
	write(t, dir, "icon.png", "\x89PNG")
	write(t, dir, "landing.html", "<h1>hi</h1>")
	s, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := substrate.SiteInfo{
		Name: "Example", Tagline: "A place for friends",
		Logo: "/site/img/logo%20one.svg", Icon: "/site/icon.png", Landing: "/site/landing.html",
	}
	if s.Info() != want {
		t.Fatalf("Info() = %+v, want %+v", s.Info(), want)
	}
	if s.Dir() != dir {
		t.Fatalf("Dir() = %q, want %q", s.Dir(), dir)
	}
}

func TestLoadNameAlone(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "site.yaml", "name: Example\n")
	s, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Info(); got != (substrate.SiteInfo{Name: "Example"}) {
		t.Fatalf("Info() = %+v, want the name alone", got)
	}
}

// Every refusal is a boot failure, so each must name what is wrong.
func TestLoadRefuses(t *testing.T) {
	outside := t.TempDir()
	write(t, outside, "secret.svg", "<svg/>")

	for _, tc := range []struct {
		name  string
		setup func(t *testing.T, dir string) string
		want  string
	}{
		{"relative dir", func(t *testing.T, dir string) string { return "site" }, "not an absolute path"},
		{"missing dir", func(t *testing.T, dir string) string { return filepath.Join(dir, "nope") }, "no such file"},
		{"dir is a file", func(t *testing.T, dir string) string {
			write(t, dir, "f", "x")
			return filepath.Join(dir, "f")
		}, "not a directory"},
		{"missing site.yaml", func(t *testing.T, dir string) string { return dir }, "site.yaml"},
		{"site.yaml is a directory", func(t *testing.T, dir string) string {
			if err := os.Mkdir(filepath.Join(dir, "site.yaml"), 0o755); err != nil {
				t.Fatal(err)
			}
			return dir
		}, "not a regular file"},
		{"empty site.yaml", func(t *testing.T, dir string) string {
			write(t, dir, "site.yaml", "")
			return dir
		}, "name is required"},
		{"missing name", func(t *testing.T, dir string) string {
			write(t, dir, "site.yaml", "tagline: hi\n")
			return dir
		}, "name is required"},
		{"blank name", func(t *testing.T, dir string) string {
			write(t, dir, "site.yaml", "name: \"   \"\n")
			return dir
		}, "name is required"},
		{"control character", func(t *testing.T, dir string) string {
			write(t, dir, "site.yaml", "name: \"a\\nb\"\n")
			return dir
		}, "control character"},
		{"unknown key", func(t *testing.T, dir string) string {
			write(t, dir, "site.yaml", "name: Example\ntagLine: hi\n")
			return dir
		}, `line 2: unknown key "tagLine" (the keys are name, tagline, logo, icon, landing)`},
		{"a list, not a mapping", func(t *testing.T, dir string) string {
			write(t, dir, "site.yaml", "- name: Example\n")
			return dir
		}, "site.yaml"},
		{"name is a mapping", func(t *testing.T, dir string) string {
			write(t, dir, "site.yaml", "name:\n  first: Example\n")
			return dir
		}, "site.yaml"},
		{"two documents", func(t *testing.T, dir string) string {
			write(t, dir, "site.yaml", "name: Example\n---\nname: Other\n")
			return dir
		}, "more than one"},
		{"not yaml", func(t *testing.T, dir string) string {
			write(t, dir, "site.yaml", "name: [\n")
			return dir
		}, "site.yaml"},
		{"missing logo", func(t *testing.T, dir string) string {
			write(t, dir, "site.yaml", "name: Example\nlogo: logo.svg\n")
			return dir
		}, "logo"},
		{"missing icon", func(t *testing.T, dir string) string {
			write(t, dir, "site.yaml", "name: Example\nicon: icon.svg\n")
			return dir
		}, "icon"},
		{"icon is not an image", func(t *testing.T, dir string) string {
			write(t, dir, "site.yaml", "name: Example\nicon: icon.html\n")
			write(t, dir, "icon.html", "<h1>x</h1>")
			return dir
		}, "not an image a browser shows"},
		{"missing landing", func(t *testing.T, dir string) string {
			write(t, dir, "site.yaml", "name: Example\nlanding: landing.html\n")
			return dir
		}, "landing"},
		{"landing is a directory", func(t *testing.T, dir string) string {
			write(t, dir, "site.yaml", "name: Example\nlanding: pages\n")
			write(t, dir, "pages/a.html", "x")
			return dir
		}, "not a regular file"},
		{"traversal", func(t *testing.T, dir string) string {
			write(t, dir, "site/site.yaml", "name: Example\nlogo: ../secret.svg\n")
			write(t, dir, "secret.svg", "<svg/>")
			return filepath.Join(dir, "site")
		}, "not a path inside"},
		{"absolute path", func(t *testing.T, dir string) string {
			write(t, dir, "site.yaml", "name: Example\nlogo: "+filepath.Join(outside, "secret.svg")+"\n")
			return dir
		}, "not a path inside"},
		{"dotfile", func(t *testing.T, dir string) string {
			write(t, dir, "site.yaml", "name: Example\nlogo: .logo.svg\n")
			write(t, dir, ".logo.svg", "<svg/>")
			return dir
		}, "not a path inside"},
		{"symlink escape", func(t *testing.T, dir string) string {
			write(t, dir, "site.yaml", "name: Example\nlogo: logo.svg\n")
			if err := os.Symlink(filepath.Join(outside, "secret.svg"), filepath.Join(dir, "logo.svg")); err != nil {
				t.Fatal(err)
			}
			return dir
		}, "logo"},
		{"site.yaml symlink escape", func(t *testing.T, dir string) string {
			write(t, outside, "site.yaml", "name: Outside\n")
			if err := os.Symlink(filepath.Join(outside, "site.yaml"), filepath.Join(dir, "site.yaml")); err != nil {
				t.Fatal(err)
			}
			return dir
		}, "site.yaml"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, err := Load(tc.setup(t, t.TempDir()))
			if err == nil {
				t.Fatalf("Load = %+v, want a refusal naming %q", s.Info(), tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Load error = %q, want it to name %q", err, tc.want)
			}
		})
	}
}

func TestTabIcon(t *testing.T) {
	for _, tc := range []struct {
		name, yaml        string
		wantHref, wantTyp string
	}{
		{"none", "name: Example\n", "", ""},
		{"logo stands in", "name: Example\nlogo: logo.svg\n", "/site/logo.svg", "image/svg+xml"},
		{"icon wins", "name: Example\nlogo: logo.svg\nicon: icon.PNG\n", "/site/icon.PNG", "image/png"},
		{"logo of no image type", "name: Example\nlogo: logo\n", "/site/logo", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			write(t, dir, "site.yaml", tc.yaml)
			write(t, dir, "logo.svg", "<svg/>")
			write(t, dir, "logo", "<svg/>")
			write(t, dir, "icon.PNG", "\x89PNG")
			s, err := Load(dir)
			if err != nil {
				t.Fatal(err)
			}
			href, typ := s.TabIcon()
			if href != tc.wantHref || typ != tc.wantTyp {
				t.Fatalf("TabIcon() = %q, %q; want %q, %q", href, typ, tc.wantHref, tc.wantTyp)
			}
		})
	}
}

// A symlink that stays inside the directory is a file in it.
func TestLoadFollowsASymlinkInside(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "site.yaml", "name: Example\nlogo: logo.svg\n")
	write(t, dir, "img/real.svg", "<svg/>")
	if err := os.Symlink("img/real.svg", filepath.Join(dir, "logo.svg")); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); err != nil {
		t.Fatal(err)
	}
}

func TestServable(t *testing.T) {
	for rel, want := range map[string]bool{
		"logo.svg":         true,
		"img/logo.svg":     true,
		"site.yaml":        true,
		"":                 false,
		"/logo.svg":        false,
		"../x":             false,
		"a/../logo.svg":    false,
		"./logo.svg":       false,
		".env":             false,
		".git/config":      false,
		"img/.hidden.png":  false,
		"img/":             false,
		"img//logo.svg":    false,
		`img\logo.svg`:     false,
		"logo.svg\x00.png": false,
	} {
		if got := Servable(rel); got != want {
			t.Errorf("Servable(%q) = %v, want %v", rel, got, want)
		}
	}
}

func TestOpenRefusesWhatItMayNotServe(t *testing.T) {
	outside := t.TempDir()
	write(t, outside, "secret.txt", "secret")
	dir := t.TempDir()
	write(t, dir, "site.yaml", "name: Example\n")
	write(t, dir, "a.txt", "a")
	write(t, dir, "img/b.png", "b")
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(dir, "out.txt")); err != nil {
		t.Fatal(err)
	}
	s, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	f, info, err := s.Open("a.txt")
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	if info.Size() != 1 {
		t.Fatalf("a.txt size = %d", info.Size())
	}
	for _, rel := range []string{"img", "out.txt", "../" + filepath.Base(outside) + "/secret.txt", "missing.txt"} {
		if f, _, err := s.Open(rel); err == nil {
			_ = f.Close()
			t.Errorf("Open(%q) succeeded, want a refusal", rel)
		}
	}
	if _, _, err := s.Open(".env"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Open(.env) = %v, want fs.ErrNotExist", err)
	}
}
