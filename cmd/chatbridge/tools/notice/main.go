// Command notice writes the chat bridge's NOTICE: every module the shipped
// binary links, its version and its license, with each distinct license
// text once. It reads the packages the linux build links (the image is
// linux), so it says the same on any machine. With -check it compares the
// file instead and fails on a difference, which is what CI runs.
//
// A license it cannot name fails it: a new dependency is read by a person
// before it ships.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type module struct {
	Path    string
	Version string
	Dir     string
	Replace *module
}

type pkg struct {
	ImportPath string
	Standard   bool
	Module     *module
}

// licenseFiles are the names a module's license is looked for under, in
// order.
var licenseFiles = []string{"LICENSE", "LICENSE.txt", "LICENSE.md", "COPYING", "LICENSE-MIT", "MIT-LICENSE"}

// classify names a license from its text.
func classify(text string) string {
	t := strings.Join(strings.Fields(text), " ")
	switch {
	case strings.Contains(t, "Permission is hereby granted, free of charge") && strings.Contains(t, "Apache License") &&
		strings.Contains(t, "Version 2.0"):
		return "MIT AND Apache-2.0"
	case strings.Contains(t, "GNU AFFERO GENERAL PUBLIC LICENSE") && strings.Contains(t, "Version 3"):
		return "AGPL-3.0"
	case strings.Contains(t, "GNU LESSER GENERAL PUBLIC LICENSE") && strings.Contains(t, "Version 3"):
		return "LGPL-3.0"
	case strings.Contains(t, "GNU GENERAL PUBLIC LICENSE") && strings.Contains(t, "Version 3"):
		return "GPL-3.0"
	case strings.Contains(t, "Mozilla Public License Version 2.0"), strings.Contains(t, "Mozilla Public License, version 2.0"):
		return "MPL-2.0"
	case strings.Contains(t, "Apache License") && strings.Contains(t, "Version 2.0"):
		return "Apache-2.0"
	case strings.Contains(t, "Permission is hereby granted, free of charge"):
		return "MIT"
	case strings.Contains(t, "Redistribution and use in source and binary forms") && regexp.MustCompile(`(?i)neither the name|names of (its|the) contributors`).MatchString(t):
		return "BSD-3-Clause"
	case strings.Contains(t, "Redistribution and use in source and binary forms"):
		return "BSD-2-Clause"
	case strings.Contains(t, "Permission to use, copy, modify, and/or distribute this software"),
		strings.Contains(t, "Permission to use, copy, modify, and distribute this software for any purpose with or without fee"):
		return "ISC"
	case strings.Contains(t, "This is free and unencumbered software released into the public domain"):
		return "Unlicense"
	}
	return ""
}

func main() {
	check := flag.Bool("check", false, "compare NOTICE with what it would say and fail on a difference")
	out := flag.String("o", "NOTICE", "the file to write or check")
	flag.Parse()
	text, err := build()
	if err != nil {
		fmt.Fprintln(os.Stderr, "notice:", err)
		os.Exit(1)
	}
	if *check {
		have, err := os.ReadFile(*out)
		if err != nil || !bytes.Equal(have, text) {
			fmt.Fprintf(os.Stderr, "notice: %s is out of date; run mise run chatbridge:notice\n", *out)
			os.Exit(1)
		}
		return
	}
	if err := os.WriteFile(*out, text, 0o644); err != nil { //nolint:gosec // NOTICE is a public file of the tree
		fmt.Fprintln(os.Stderr, "notice:", err)
		os.Exit(1)
	}
}

func build() ([]byte, error) {
	cmd := exec.Command("go", "list", "-deps", "-json=ImportPath,Standard,Module", ".")
	cmd.Env = append(os.Environ(), "GOOS=linux", "GOARCH=amd64", "CGO_ENABLED=1")
	cmd.Stderr = os.Stderr
	raw, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("listing the binary's packages: %w", err)
	}
	mods := map[string]*module{}
	dec := json.NewDecoder(bytes.NewReader(raw))
	for {
		var p pkg
		if err := dec.Decode(&p); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return nil, err
		}
		if p.Standard || p.Module == nil || p.Module.Path == "github.com/geoah/substrate/cmd/chatbridge" {
			continue
		}
		m := p.Module
		if m.Replace != nil {
			m = &module{Path: m.Path, Version: m.Replace.Version, Dir: m.Replace.Dir}
		}
		mods[m.Path] = m
	}
	paths := make([]string, 0, len(mods))
	for p := range mods {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	type entry struct {
		path, version, license, file string
	}
	var entries []entry
	texts := map[string]string{}
	textModules := map[string][]string{}
	var unknown []string
	for _, p := range paths {
		m := mods[p]
		file, text := findLicense(m.Dir)
		id := classify(text)
		if id == "" {
			unknown = append(unknown, p)
			continue
		}
		sum := fmt.Sprintf("%x", sha256.Sum256([]byte(strings.TrimSpace(text))))
		texts[sum] = strings.TrimSpace(text)
		textModules[sum] = append(textModules[sum], p+" "+m.Version)
		entries = append(entries, entry{p, m.Version, id, file})
	}
	// The standard library is linked into every Go binary.
	goroot, err := exec.Command("go", "env", "GOROOT").Output()
	if err != nil {
		return nil, err
	}
	goversion, err := exec.Command("go", "env", "GOVERSION").Output()
	if err != nil {
		return nil, err
	}
	if _, text := findLicense(strings.TrimSpace(string(goroot))); text != "" {
		sum := fmt.Sprintf("%x", sha256.Sum256([]byte(strings.TrimSpace(text))))
		name := "Go standard library " + strings.TrimSpace(string(goversion))
		texts[sum] = strings.TrimSpace(text)
		textModules[sum] = append(textModules[sum], name)
		entries = append(entries, entry{"Go standard library", strings.TrimSpace(string(goversion)), classify(text), "LICENSE"})
	}
	if len(unknown) > 0 {
		return nil, fmt.Errorf("no license this tool can name in %s; read it and teach this tool its name", strings.Join(unknown, ", "))
	}

	var b bytes.Buffer
	b.WriteString(`The chat bridge (cmd/chatbridge of github.com/geoah/substrate) is free
software under the GNU Affero General Public License, version 3: see
LICENSE. Its source, at the commit an image was built from, is the commit
the image's org.opencontainers.image.revision label names.

It links the modules below, each under its own license. This file is
written by tools/notice (mise run chatbridge:notice) from the packages the
linux build links, and CI checks it is current.

Module                                                           License
`)
	for _, e := range entries {
		fmt.Fprintf(&b, "%-64s %s\n", e.path+" "+e.version, e.license)
	}
	sums := make([]string, 0, len(texts))
	for s := range texts {
		sums = append(sums, s)
	}
	sort.Slice(sums, func(i, j int) bool { return textModules[sums[i]][0] < textModules[sums[j]][0] })
	for _, s := range sums {
		b.WriteString("\n" + strings.Repeat("=", 78) + "\n")
		for _, m := range textModules[s] {
			b.WriteString(m + "\n")
		}
		b.WriteString(strings.Repeat("-", 78) + "\n\n")
		b.WriteString(texts[s])
		b.WriteString("\n")
	}
	return b.Bytes(), nil
}

// findLicense reads a module's license file.
func findLicense(dir string) (string, string) {
	for _, name := range licenseFiles {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err == nil {
			return name, string(raw)
		}
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(strings.ToUpper(e.Name()), "LICEN") && !e.IsDir() {
			raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err == nil {
				return e.Name(), string(raw)
			}
		}
	}
	return "", ""
}
