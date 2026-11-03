package catalog

import (
	"strings"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// Settings names empty configuration records owned by this closure. Values
// stay inside the closure and are never copied into approval metadata.
func (b *Bundle) Settings() []substrate.AuthorizationSetting {
	var out []substrate.AuthorizationSetting
	for _, doc := range b.dataDocs {
		kind, _ := doc["kind"].(string)
		if kind != vocabulary.PackageCore+"/setting" && kind != vocabulary.PackageCore+"/secret" {
			continue
		}
		metadata, _ := doc["metadata"].(map[string]any)
		id, _ := metadata["id"].(string)
		if !strings.HasPrefix(id, b.ID+"/") {
			continue
		}
		data, _ := doc["data"].(map[string]any)
		props, _ := data["properties"].(map[string]any)
		if value, _ := props["value"].(string); value != "" {
			continue
		}
		description, _ := props["description"].(string)
		required, _ := props["required"].(bool)
		out = append(out, substrate.AuthorizationSetting{ID: id, Description: description, Required: required, Secret: kind == vocabulary.PackageCore+"/secret"})
	}
	return out
}
