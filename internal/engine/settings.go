package engine

// A bundle's configuration as ORDINARY RECORDS (decision record 0076): core
// `setting` and `secret` rows whose ids sit under the bundle's own id, which
// the bundle ships empty in its closure and the user fills in.
//
// Ownership is the ID PREFIX and nothing else — `<authority>/<package>/` — so
// a sample's settings rehome with the rest of its closure and two bundles may
// both own an `apiKey`. The engine reads that prefix in exactly three places,
// and they are all here: injection into a function's `config.settings`, the
// bundle's setup status, and purge. A hand-written record under another
// bundle's prefix is that bundle's setting; the convention IS the ownership.

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	// kindSetting and kindSecret are the two shapes a bundle's configuration
	// takes: one string the user fills, and one credential sealed at rest.
	// Neither is a system kind — the whole point is that the owner writes
	// them through the ordinary surface.
	kindSetting = "substrate.reamde.dev/core/setting"
	kindSecret  = "substrate.reamde.dev/core/secret"

	propSettingValue       = "value"
	propSettingType        = "type"
	propSettingValues      = "values"
	propSettingRequired    = "required"
	propSettingDisplayName = "displayName"

	// The `type` hints a setting's value is held to. `string` admits
	// anything, which is why it is the declared default.
	settingTypeString = "string"
	settingTypeURL    = "url"
	settingTypeInt    = "int"
	settingTypeBool   = "bool"
	settingTypeEnum   = "enum"
)

// settingPrefix is the id prefix a bundle's settings and secrets live under.
// The bundle id is already `<authority>/<package>`, so this is that plus the
// separator the name follows.
func settingPrefix(b *vocabulary.Bundle) string { return b.Identity() + "/" }

// settingName is the part of a setting's id that names it — what the bundle's
// functions read it as under `config.settings`. It is empty when the id does
// not sit under the prefix.
func settingName(prefix, id string) string {
	name, ok := strings.CutPrefix(id, prefix)
	if !ok || name == "" || strings.Contains(name, "/") {
		return ""
	}
	return name
}

// guardSettingWrite holds a `setting`'s value to the `type` it declares. It
// runs inside apply, beside the other per-kind write guards, so it sees the
// write's own properties over the stored ones: a put or a patch naming only
// `value` is validated against the STORED type, and one naming only `type` is
// validated against the stored value, because either move can break the pair.
//
// An EMPTY value is always admitted: empty is the unfilled state every shipped
// setting starts in, and a bundle could not ship one otherwise.
func (t *txn) guardSettingWrite(sp *applySpec) error {
	if sp.ty.Identity == kindSecret {
		// The invocation scrubber ignores a value shorter than scrubMinLen,
		// because a two-letter pattern would redact half of every output.
		// A secret that short would therefore cross the runner boundary
		// unscrubbed, so it is refused here rather than admitted and leaked.
		if v := settingEffective(sp, propSettingValue); v != "" && len(v) < scrubMinLen {
			return fmt.Errorf("%w: secret %s: a value shorter than %d characters cannot be held to the runner boundary",
				substrate.ErrValidation, propSettingValue, scrubMinLen)
		}
		return nil
	}
	if sp.ty.Identity != kindSetting {
		return nil
	}
	value := settingEffective(sp, propSettingValue)
	if value == "" {
		return nil
	}
	switch settingEffective(sp, propSettingType) {
	case settingTypeURL:
		u, err := url.Parse(value)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return fmt.Errorf("%w: setting %s: value %q is not an absolute URL, and the setting's type is url",
				substrate.ErrValidation, propSettingValue, value)
		}
	case settingTypeInt:
		if _, err := strconv.ParseInt(value, 10, 64); err != nil {
			return fmt.Errorf("%w: setting %s: value %q is not a whole number, and the setting's type is int",
				substrate.ErrValidation, propSettingValue, value)
		}
	case settingTypeBool:
		// The two spellings a bool reads back as, and nothing looser: a
		// setting is one string a body parses, so "yes" would have to be a
		// second contract nothing else in the tree keeps.
		if value != "true" && value != "false" {
			return fmt.Errorf("%w: setting %s: value %q is not true or false, and the setting's type is bool",
				substrate.ErrValidation, propSettingValue, value)
		}
	case settingTypeEnum:
		admitted := settingEffectiveList(sp, propSettingValues)
		if !slices.Contains(admitted, value) {
			return fmt.Errorf("%w: setting %s: value %q is not one of %v, and the setting's type is enum",
				substrate.ErrValidation, propSettingValue, value, admitted)
		}
	}
	return nil
}

// settingEffective reads the string a property WILL hold once this write
// commits: what the write names, else what the row already stores.
func settingEffective(sp *applySpec, name string) string {
	if v, named := sp.props[name]; named {
		s, _ := v.(string)
		return s
	}
	if sp.existing == nil {
		return ""
	}
	s, _ := sp.existing.Props[name].(string)
	return s
}

// settingEffectiveList is settingEffective for a repeated string property,
// read through the one reader a repeated property has (policy.go stringList).
func settingEffectiveList(sp *applySpec, name string) []string {
	if v, named := sp.props[name]; named {
		return stringList(v)
	}
	if sp.existing == nil {
		return nil
	}
	return stringList(sp.existing.Props[name])
}

// bundleSettings resolves one bundle's settings and secrets into the map its
// functions read as `config.settings`, plus the plaintext secrets the
// invocation scrubber holds every outbound surface to. Both come back from
// ONE pass over each row (injectedRecordConfig), so the value injected and the
// value scrubbed cannot disagree.
//
// A secret that will not open is OMITTED rather than injected as ciphertext:
// the body then sees the key absent and refuses in its own words, which is
// what an unresolved input does too.
func (ds *dataset) bundleSettings(ctx context.Context, b *vocabulary.Bundle) (map[string]any, []string, error) {
	prefix := settingPrefix(b)
	out := map[string]any{}
	var secrets []string
	for _, ident := range []string{kindSetting, kindSecret} {
		ty, ok := ds.registry().ByIdentity(ident)
		if !ok {
			continue
		}
		rows, err := ds.liveRowsOf(ctx, ident)
		if err != nil {
			return nil, nil, err
		}
		for _, row := range rows {
			name := settingName(prefix, row.ID)
			if name == "" {
				continue
			}
			view, rsecrets := ds.injectedRecordConfig(ctx, ty, row)
			props, _ := view["properties"].(map[string]any)
			value, held := props[propSettingValue]
			if !held {
				continue
			}
			out[name] = value
			secrets = append(secrets, rsecrets...)
		}
	}
	return out, secrets, nil
}

// settingSetupItems lists the bundle's REQUIRED settings and secrets that are
// still empty — the same refusal a body makes when it reaches for a key
// nobody filled in, moved to the status page. A secret's emptiness is read
// the way the OAuth client's completeness is: the stored string is the sealed
// reference, so an absent one is an absent value and nothing has to be
// opened to know it.
func (ds *dataset) settingSetupItems(ctx context.Context, b *vocabulary.Bundle) ([]substrate.SetupItem, error) {
	prefix := settingPrefix(b)
	var out []substrate.SetupItem
	for _, ident := range []string{kindSetting, kindSecret} {
		rows, err := ds.liveRowsOf(ctx, ident)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			name := settingName(prefix, row.ID)
			if name == "" {
				continue
			}
			if required, _ := row.Props[propSettingRequired].(bool); !required {
				continue
			}
			if propString(row, propSettingValue) != "" {
				continue
			}
			label := propString(row, propSettingDisplayName)
			if label == "" {
				label = name
			}
			out = append(out, substrate.SetupItem{
				Code: substrate.SetupSetting, Kind: ident, Record: row.ID,
				Message: fmt.Sprintf("%s is not set", label),
			})
		}
	}
	return out, nil
}

// purgeSettings tombstones the bundle's settings and secrets, which purge owes
// and uninstall does not: purge removes the bundle's data, and its
// configuration is data it owns. They are ordinary records, so this is the
// ordinary soft delete, batched exactly as purgeTypes batches a kind.
func (ds *dataset) purgeSettings(ctx context.Context, b *vocabulary.Bundle) (int, error) {
	prefix := settingPrefix(b)
	purged := 0
	for _, ident := range []string{kindSetting, kindSecret} {
		rows, err := ds.liveRowsOf(ctx, ident)
		if err != nil {
			return purged, err
		}
		var ids []string
		for _, row := range rows {
			if settingName(prefix, row.ID) != "" {
				ids = append(ids, row.ID)
			}
		}
		for chunk := range slices.Chunk(ids, gcBatch) {
			err := ds.inTx(ctx, substrate.ActorSystem, true, func(t *txn) error {
				for _, id := range chunk {
					if _, err := t.softDelete(eref{Kind: ident, ID: id}); err != nil {
						return err
					}
				}
				return nil
			})
			if err != nil {
				return purged, err
			}
			purged += len(chunk)
		}
	}
	return purged, nil
}
