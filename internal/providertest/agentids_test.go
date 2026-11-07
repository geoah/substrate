package providertest

import (
	"io/fs"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/vocabulary"
)

// composedID is a host helper that names a record by a hash of its upstream
// key in a shape that is not minted: right for a sync function, which writes
// as itself and names its records (decision 0162), refused for a body run as
// an agent's tool, which writes as the agent (decision 0228).
var composedID = regexp.MustCompile(`host\.ids\.(url|external)\(`)

// Every shipped function an agent can call (offered to agents, or named in a
// shipped agent's tools) names a new record of a minted kind with
// host.ids.minted, so it lands when an agent calls it. web_scrape kept its
// page under host.ids.url, and every scrape an agent asked for was refused.
func TestAnAgentsToolNamesNewRecordsTheWayAnAgentMay(t *testing.T) {
	t.Parallel()
	type fn struct {
		id, file, source string
		offered          bool
		writes           []string
		modules          map[string]string
	}
	named := map[string]bool{}
	var fns []fn
	called := map[string]bool{}
	for _, root := range []string{kindsDir, samplesDir} {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || filepath.Ext(path) != ".yaml" {
				return err
			}
			modules := map[string]string{}
			var here []fn
			for _, doc := range loadDocs(t, path) {
				meta, _ := doc["metadata"].(map[string]any)
				id, _ := meta["id"].(string)
				data, _ := doc["data"].(map[string]any)
				switch doc["kind"] {
				case typeBundle:
					for name, body := range asMap(data["modules"]) {
						modules[strings.TrimSuffix(name, ".py")], _ = body.(string)
					}
				case typeKind:
					ids, _ := asMap(data["ids"])["type"].(string)
					named[id] = ids == vocabulary.IDsNamed || (ids == "" && data["purpose"] == vocabulary.PurposeInternal)
				case typeFunction:
					src, _ := data["source"].(string)
					var writes []string
					for _, w := range asList(asMap(data["permissions"])["writes"]) {
						if s, ok := w.(string); ok {
							writes = append(writes, s)
						}
					}
					here = append(here, fn{
						id: id, file: path, source: src, writes: writes,
						offered: asMap(data["offer"])["agents"] == true,
					})
				case vocabulary.CoreKind(vocabulary.DocAgent):
					for _, tool := range asList(data["tools"]) {
						if f, ok := asMap(tool)["function"].(string); ok {
							called[f] = true
						}
					}
				}
			}
			for i := range here {
				here[i].modules = modules
			}
			fns = append(fns, here...)
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}
	checked := 0
	for _, f := range fns {
		if !f.offered && !called[f.id] {
			continue
		}
		checked++
		body := f.source
		for name, module := range f.modules {
			if strings.Contains(f.source, name) {
				body += "\n" + module
			}
		}
		if !composedID.MatchString(body) {
			continue
		}
		minted := slices.DeleteFunc(slices.Clone(f.writes), func(k string) bool { return named[k] })
		if len(minted) > 0 {
			t.Errorf("%s (%s) is an agent's tool and names new records with host.ids.url or host.ids.external, "+
				"which an agent may not choose for %v; derive the id with host.ids.minted(<key>)", f.id, f.file, minted)
		}
	}
	if checked == 0 {
		t.Fatal("no shipped function is an agent's tool: the walk read nothing")
	}
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}
