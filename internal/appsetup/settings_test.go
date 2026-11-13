package appsetup

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/geoah/substrate/internal/catalog"
	"github.com/geoah/substrate/internal/substrate"
)

func TestApprovalSettingsAreMetadataAndOwnerWrites(t *testing.T) {
	const id = "example.com/configured"
	cat, err := catalog.Load(catalog.ProviderRoot(fstest.MapFS{"configured/bundle.yaml": {Data: []byte(closure(id, 1, nil, `kind: substrate.reamde.dev/core/secret
metadata:
  id: example.com/configured/key
data:
  properties:
    description: The credential
    required: true
`))}}))
	if err != nil {
		t.Fatal(err)
	}
	ds := newFakeDataset()
	details := []substrate.AuthorizationDetail{{Type: substrate.DetailPackage, Package: id, Version: 1}}
	rows, hash, err := Plan(context.Background(), ds, cat, details, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows[0].Package.Settings) != 1 {
		t.Fatalf("settings: %+v", rows)
	}
	if err := ValidateSettings(rows, nil); err == nil || !strings.Contains(err.Error(), substrate.BlockNeedsSetting) {
		t.Fatalf("required empty: %v", err)
	}
	missing := NewRunner(cat).Run(context.Background(), ds, substrate.ActorPerson, details, rows, everything(), RunOptions{}, func([]string) bool { return true })
	if missing.Failure == nil || missing.Failure.Message != "Check the settings and add-ons, then approve again." {
		t.Fatalf("missing settings failure: %+v", missing.Failure)
	}
	values := []substrate.AuthorizationSettingValue{{ID: id + "/key", Secret: true, Value: "test-only-private-value"}}
	if err := ValidateSettings(rows, values); err != nil {
		t.Fatal(err)
	}
	duplicate := []substrate.AuthorizationSettingValue{{ID: id + "/key", Secret: true}, {ID: id + "/key", Secret: true, Value: "second"}}
	if err := ValidateSettings(rows, duplicate); err == nil {
		t.Fatal("repeated empty setting admitted")
	}
	wrongKind := []substrate.AuthorizationSettingValue{{ID: id + "/key", Value: "test-only-private-value"}}
	if err := ValidateSettings(rows, wrongKind); err == nil {
		t.Fatal("secret admitted as plain setting")
	}
	var captured bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&captured, nil)))
	defer slog.SetDefault(old)
	out := NewRunner(cat).Run(context.Background(), ds, substrate.ActorPerson, details, rows, everything(), RunOptions{Settings: values}, func([]string) bool { return true })
	if out.Failure != nil {
		t.Fatal(out.Failure)
	}
	rec, err := ds.Get(context.Background(), settingKind(true), id+"/key")
	if err != nil || rec.Properties["value"] != values[0].Value {
		t.Fatalf("write: %v %v", rec, err)
	}
	again, _ := Hash(rows)
	if again != hash {
		t.Fatal("values changed plan hash")
	}
	raw, _ := json.Marshal(rows)
	if strings.Contains(string(raw)+captured.String(), values[0].Value) {
		t.Fatal("secret leaked")
	}
	ds.putErr = func(substrate.PutInput) error { return errors.New(values[0].Value) }
	err = WriteSettings(context.Background(), ds, substrate.ActorPerson, values)
	if err == nil || strings.Contains(err.Error()+captured.String(), values[0].Value) {
		t.Fatal("write error leaked")
	}
	if err := WriteSettings(context.Background(), ds, substrate.Actor("app:example.com"), values); !errors.Is(err, substrate.ErrForbidden) {
		t.Fatalf("app write: %v", err)
	}
}

func TestOptionalAddonsRetainOnlyTheOwnersSelection(t *testing.T) {
	rows := []substrate.AuthorizationSetup{{Type: substrate.DetailPackage, Action: substrate.SetupWait, Package: &substrate.AuthorizationPackage{ID: appPkg, Required: 4, AddonFor: basePkg}}}
	selected, err := SelectAddons(rows, []string{appPkg})
	if err != nil || len(WaitingAddons(selected)) != 1 {
		t.Fatalf("selected: %v %v", selected, err)
	}
	omitted, err := SelectAddons(rows, nil)
	if err != nil || omitted[0].Action != substrate.SetupNone || len(WaitingAddons(omitted)) != 0 {
		t.Fatalf("omitted: %v %v", omitted, err)
	}
	if rows[0].Action != substrate.SetupWait {
		t.Fatal("selection mutated plan")
	}
	if _, err := SelectAddons(rows, []string{basePkg}); err == nil {
		t.Fatal("unrequested add-on admitted")
	}
}
