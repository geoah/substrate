package appsetup

import (
	"context"
	"errors"
	"fmt"

	"github.com/geoah/substrate/internal/catalog"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

func settingKind(secret bool) string {
	if secret {
		return vocabulary.PackageCore + "/secret"
	}
	return vocabulary.PackageCore + "/setting"
}

func (p *planner) emptySettings(ctx context.Context, b *catalog.Bundle) ([]substrate.AuthorizationSetting, error) {
	var out []substrate.AuthorizationSetting
	for _, s := range b.Settings() {
		rec, err := p.ds.Get(ctx, settingKind(s.Secret), s.ID)
		if err != nil && !errors.Is(err, substrate.ErrNotFound) {
			return nil, err
		}
		if err == nil && rec.DeletedAt == nil && rec.Properties["value"] != nil && rec.Properties["value"] != "" {
			continue
		}
		out = append(out, s)
	}
	return out, nil
}

// ValidateSettings admits only fields the plan showed. Identity includes the
// kind so a secret cannot accidentally be written into a readable setting.
func ValidateSettings(rows []substrate.AuthorizationSetup, values []substrate.AuthorizationSettingValue) error {
	allowed := map[string]substrate.AuthorizationSetting{}
	for _, row := range rows {
		if row.Package == nil || row.Action == substrate.SetupWait {
			continue
		}
		for _, s := range row.Package.Settings {
			allowed[settingKind(s.Secret)+"/"+s.ID] = s
		}
	}
	seen := map[string]bool{}
	for _, v := range values {
		key := settingKind(v.Secret) + "/" + v.ID
		_, duplicate := seen[key]
		if _, ok := allowed[key]; !ok || duplicate {
			return fmt.Errorf("%w: unrecognized or repeated setting", substrate.ErrValidation)
		}
		seen[key] = v.Value != ""
	}
	for key, s := range allowed {
		if s.Required && !seen[key] {
			return fmt.Errorf("%w: %s: fill every required setting", substrate.ErrValidation, substrate.BlockNeedsSetting)
		}
	}
	return nil
}

// WriteSettings uses the ordinary owner write path, which seals secret
// properties. No underlying error escapes: validators may quote the input.
func WriteSettings(ctx context.Context, ds substrate.Dataset, actor substrate.Actor, values []substrate.AuthorizationSettingValue) (err error) {
	defer func() {
		if recover() != nil {
			err = errors.New("a setting could not be saved")
		}
	}()
	if !substrate.IsPerson(actor) {
		return substrate.ErrForbidden
	}
	for _, v := range values {
		if v.Value == "" {
			continue
		}
		_, err := ds.Put(ctx, actor, substrate.PutInput{Kind: settingKind(v.Secret), ID: v.ID, Properties: map[string]any{"value": v.Value}})
		if err != nil {
			return errors.New("a setting could not be saved; check its required format and try again")
		}
	}
	return nil
}
