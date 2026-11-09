package engine

import (
	"reflect"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// A bundle's feature toggles reach its status in toggle order, each with its
// scopes and its access, an undeclared access reading as `read` (decision
// record 0255). A bundle with no oauth2 block lists none.
func TestBundleFeaturesListEachToggle(t *testing.T) {
	t.Parallel()
	b := &vocabulary.Bundle{OAuth2: &vocabulary.BundleOAuth2{
		FeatureScopes: map[string][]string{
			"enabledMail":      {"mail.read"},
			"enabledMailWrite": {"mail.modify", "profile"},
		},
		FeatureAccess: map[string]string{"enabledMailWrite": vocabulary.FeatureAccessWrite},
	}}
	want := []substrate.BundleFeature{
		{Toggle: "enabledMail", Scopes: []string{"mail.read"}, Access: "read"},
		{Toggle: "enabledMailWrite", Scopes: []string{"mail.modify", "profile"}, Access: "write"},
	}
	if got := bundleFeatures(b); !reflect.DeepEqual(got, want) {
		t.Fatalf("bundleFeatures = %+v, want %+v", got, want)
	}
	if got := bundleFeatures(&vocabulary.Bundle{}); got != nil {
		t.Fatalf("a bundle with no oauth2 block listed %+v", got)
	}
}
