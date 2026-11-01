package vocabulary

import (
	"slices"
	"strings"
)

// TriggerKind is the kind a trigger record is: what fires a function or an
// agent on a record change, a schedule or a webhook delivery.
const TriggerKind = PackageCore + "/trigger"

// The two callable kinds a trigger's `callable` reference names, each
// followed by the callable's own `<authority>/<package>/<name>`.
var callableKinds = []string{PackageCore + "/" + DocFunction + "/", PackageCore + "/" + DocAgent + "/"}

// CallablePackage is the package a trigger's callable belongs to:
// `substrate.reamde.dev/core/function/<authority>/<package>/<name>` names
// `<authority>/<package>`, and an agent's reference the same. Empty for
// anything else. It is what decides whose trigger an id is, for the engine's
// install and for an approval that turns a webhook on, so the two cannot
// disagree.
func CallablePackage(callable string) string {
	for _, kind := range callableKinds {
		if rest, ok := strings.CutPrefix(callable, kind); ok {
			if parts := strings.SplitN(rest, "/", 3); len(parts) == 3 {
				return parts[0] + "/" + parts[1]
			}
		}
	}
	return ""
}

// ReferencePath reads a reference value in either shape: the `{ref: …}`
// object every write stores, or the bare path a manifest authors.
func ReferencePath(v any) string { return referentPath(v) }

// TriggerWebhook is a trigger's webhook source as its properties hold it,
// nil when it has none. The map is the caller's to read, never to change.
func TriggerWebhook(props map[string]any) map[string]any {
	source, _ := props["source"].(map[string]any)
	webhook, _ := source["webhook"].(map[string]any)
	return webhook
}

// TriggerKey is a trigger's webhook key, empty when it has none.
func TriggerKey(props map[string]any) string {
	key, _ := TriggerWebhook(props)["key"].(string)
	return key
}

// TriggerSigned reports whether a trigger's webhook source declares a
// signature: its provider signs what it sends, with a secret the provider
// and the owner share, and the door checks that rather than a key.
func TriggerSigned(props map[string]any) bool {
	return TriggerWebhook(props)["signature"] != nil
}

// ValidTriggerID reports whether s names one trigger the way the webhook door
// and a `webhook:` scope do: a record id that is one path segment, so no
// `/`. The id alphabet admits no `*`, so no trigger id is a glob.
func ValidTriggerID(s string) bool { return ValidID(s) && !strings.Contains(s, "/") }

// BindsTrait reports whether a kind declaration's `traits:` binds trait, a
// trait's full identity, whatever variant or columns it binds it with. It
// reads the declaration as stored, where every binding is spelled in full
// (decision 0101), so it needs no registry to resolve one.
func BindsTrait(def map[string]any, trait string) bool {
	return slices.ContainsFunc(mslice(def, "traits"), func(v any) bool {
		s, _ := v.(string)
		m := reCapBinding.FindStringSubmatch(strings.TrimSpace(s))
		return m != nil && m[1] == trait
	})
}
