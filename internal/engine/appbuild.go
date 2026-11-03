package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

func validateAppBuild(packages map[string]int64, version string) error {
	if utf8.RuneCountInString(version) > 64 {
		return fmt.Errorf("%w: softwareVersion exceeds 64 characters", substrate.ErrValidation)
	}
	for _, r := range version {
		if !unicode.IsPrint(r) {
			return fmt.Errorf("%w: softwareVersion must be printable", substrate.ErrValidation)
		}
	}
	for id, floor := range packages {
		a, p := vocabulary.SplitPackageRef(id)
		if !vocabulary.ValidAuthority(a) || !vocabulary.ValidPackage(p) || floor < 1 {
			return fmt.Errorf("%w: invalid package floor for %s", substrate.ErrValidation, id)
		}
	}
	return nil
}

func appCheckTime(client string) *time.Time {
	if client == "" {
		return nil
	}
	at := nowUTC()
	return &at
}

func readAppBuild(raw []byte, info *substrate.TokenInfo) error {
	var build struct {
		Packages map[string]int64     `json:"appPackages"`
		Software string               `json:"appSoftwareVersion"`
		Checked  *time.Time           `json:"appCheckedAt"`
		Addons   []substrate.AppAddon `json:"appAddons"`
	}
	if err := json.Unmarshal(raw, &build); err != nil {
		return fmt.Errorf("%w: token %s build facts: %w", substrate.ErrCorrupt, info.ID, err)
	}
	if err := validateAppBuild(build.Packages, build.Software); err != nil {
		return err
	}
	info.AppPackages, info.AppSoftwareVersion, info.AppCheckedAt, info.AppAddons = build.Packages, build.Software, build.Checked, build.Addons
	return nil
}

// RecordAppBuild writes only the authenticated token, with the same transaction
// and managed-property guard as other engine-owned facts.
func (ds *dataset) RecordAppBuild(ctx context.Context, packages map[string]int64, version string) error {
	if err := validateAppBuild(packages, version); err != nil {
		return err
	}
	id := substrate.PrincipalFrom(ctx)
	if id == "" {
		return fmt.Errorf("%w: an app token is required", substrate.ErrForbidden)
	}
	return ds.inTx(ctx, substrate.ActorSystem, true, func(t *txn) error {
		row, err := t.loadRow(eref{Kind: kindToken, ID: id}, true)
		if err != nil {
			return err
		}
		if row == nil || row.DeletedAt != nil || row.Props["clientId"] == nil {
			return fmt.Errorf("%w: a live app token is required", substrate.ErrForbidden)
		}
		raw, err := json.Marshal(row.Props)
		if err != nil {
			return err
		}
		var held substrate.TokenInfo
		if err := readAppBuild(raw, &held); err != nil {
			return err
		}
		// Floors enforce build holds; check-in time only needs daily granularity.
		if maps.Equal(held.AppPackages, packages) && held.AppSoftwareVersion == version &&
			held.AppCheckedAt != nil && t.now.Sub(*held.AppCheckedAt) < 24*time.Hour {
			return nil
		}
		_, err = t.put(substrate.PutInput{Kind: kindToken, ID: id, Properties: map[string]any{"appPackages": appPackageValues(packages), "appSoftwareVersion": version, "appCheckedAt": t.now.Format(time.RFC3339Nano)}})
		return err
	})
}

// The repository write transaction serializes token check-ins and upgrades.
// A catalog preview alone cannot enforce a hold across a concurrent check-in.
func (t *txn) checkPublishedHolders(pkg string, version int64) error {
	rows, err := t.tx.QueryContext(t.ctx, "SELECT id, props FROM records WHERE kind=$1 AND deleted_at IS NULL", kindToken)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var info substrate.TokenInfo
		var raw []byte
		if err := rows.Scan(&info.ID, &raw); err != nil {
			return err
		}
		var props struct {
			ExpiresAt  *time.Time `json:"expiresAt"`
			ClientName string     `json:"clientName"`
		}
		if err := json.Unmarshal(raw, &props); err != nil {
			return err
		}
		if props.ExpiresAt != nil && !props.ExpiresAt.After(t.now) {
			continue
		}
		if err := readAppBuild(raw, &info); err != nil {
			return err
		}
		if floor, ok := info.AppPackages[pkg]; ok && floor < version {
			return fmt.Errorf("%w: %s holds %s at floor %d below version %d", substrate.ErrConflict, props.ClientName, pkg, floor, version)
		}
	}
	return rows.Err()
}

func appPackageValues(packages map[string]int64) map[string]any {
	out := make(map[string]any, len(packages))
	for id, v := range packages {
		out[id] = v
	}
	return out
}
