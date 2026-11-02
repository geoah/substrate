package authorize

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/geoah/substrate/internal/scope"
	"github.com/geoah/substrate/internal/strictjson"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// An app's `authorization_details` (RFC 9396) name what approving sets up:
// catalog packages at a version or later, the account it delivers under and
// the webhook it delivers to (docs/plans/app-setup.md). A parked request
// holds them in memory and the approval page lists each, so they are
// bounded like `scope`.
const (
	maxDetails      = 8
	maxDetailsBytes = 2048
	// maxAccountProperties bounds an account's properties as JSON: every
	// value is shown to the owner before it is written.
	maxAccountProperties = 1024
	// inputDefaultID is the record id a bundle input resolves first.
	inputDefaultID = "default"
)

// The entry shapes a request may send, one per type. Each is decoded
// strictly, so a member another type carries, or `url` (the token
// response's), refuses the request.
type (
	packageEntry struct {
		Type    string `json:"type"`
		Package string `json:"package"`
		Version int64  `json:"version"`
	}
	accountEntry struct {
		Type       string         `json:"type"`
		Kind       string         `json:"kind"`
		ID         string         `json:"id"`
		Properties map[string]any `json:"properties"`
	}
	webhookEntry struct {
		Type    string `json:"type"`
		Trigger string `json:"trigger"`
	}
	agentEntry struct {
		Type             string   `json:"type"`
		Agent            string   `json:"agent"`
		Reads            []string `json:"reads"`
		Writes           []string `json:"writes"`
		SpendCentsPerDay *int     `json:"spendCentsPerDay"`
	}
)

// An agent detail's lists are bounded so the approval page can name every
// kind: a coach reads a handful of packages, not the repository.
const (
	maxAgentReads  = 24
	maxAgentWrites = 16
)

// ParseDetails reads an authorization request's `authorization_details`: a
// JSON array of at most 8 entries in at most 2 KB. Each entry is read
// strictly against its type's members, and the whole is held to
// checkDetails. An empty parameter is no details. Numbers in an account's
// properties stay json.Number, as the REST door reads a record body. It is
// the one reading of details: /oauth/authorize reads its parameter with it
// and the set up check its body's `details`, so the check never admits what
// an authorization request would be refused.
func ParseDetails(param string) ([]substrate.AuthorizationDetail, error) {
	if param == "" {
		return nil, nil
	}
	if len(param) > maxDetailsBytes {
		return nil, fmt.Errorf("authorization_details is longer than %d bytes", maxDetailsBytes)
	}
	var entries []json.RawMessage
	if err := json.Unmarshal([]byte(param), &entries); err != nil || entries == nil {
		return nil, errors.New("authorization_details must be a JSON array of objects")
	}
	if len(entries) > maxDetails {
		return nil, fmt.Errorf("authorization_details names more than %d entries", maxDetails)
	}
	out := make([]substrate.AuthorizationDetail, 0, len(entries))
	for i, raw := range entries {
		d, err := parseDetail(raw)
		if err != nil {
			return nil, fmt.Errorf("authorization_details[%d]: %w", i, err)
		}
		out = append(out, d)
	}
	if err := checkDetails(out); err != nil {
		return nil, err
	}
	return out, nil
}

func parseDetail(raw json.RawMessage) (substrate.AuthorizationDetail, error) {
	var members map[string]json.RawMessage
	if err := json.Unmarshal(raw, &members); err != nil || members == nil {
		return substrate.AuthorizationDetail{}, errors.New("is not a JSON object")
	}
	if _, ok := members["url"]; ok {
		return substrate.AuthorizationDetail{}, errors.New("url is the token response's to answer, never a request's to send")
	}
	var typ string
	if err := json.Unmarshal(members["type"], &typ); err != nil {
		return substrate.AuthorizationDetail{}, errors.New("type must be package, account, webhook or agent")
	}
	var d substrate.AuthorizationDetail
	var err error
	switch typ {
	case substrate.DetailPackage:
		var e packageEntry
		err = strictjson.DecodeBytes(raw, &e, true)
		d = substrate.AuthorizationDetail{Type: e.Type, Package: e.Package, Version: e.Version}
	case substrate.DetailAccount:
		var e accountEntry
		err = strictjson.DecodeBytes(raw, &e, true)
		if err == nil && members["properties"] != nil && e.Properties == nil {
			err = errors.New("properties must be an object")
		}
		d = substrate.AuthorizationDetail{Type: e.Type, Kind: e.Kind, ID: e.ID, Properties: e.Properties}
	case substrate.DetailWebhook:
		var e webhookEntry
		err = strictjson.DecodeBytes(raw, &e, true)
		d = substrate.AuthorizationDetail{Type: e.Type, Trigger: e.Trigger}
	case substrate.DetailAgent:
		var e agentEntry
		err = strictjson.DecodeBytes(raw, &e, true)
		if err == nil && e.SpendCentsPerDay == nil {
			err = errors.New("spendCentsPerDay is required: the most the agent may spend a day, in cents")
		}
		if err == nil {
			d = substrate.AuthorizationDetail{Type: e.Type, Agent: e.Agent, Reads: e.Reads, Writes: e.Writes, SpendCentsPerDay: *e.SpendCentsPerDay}
		}
	default:
		return substrate.AuthorizationDetail{}, fmt.Errorf("type %q is not package, account, webhook or agent", typ)
	}
	if err != nil {
		return substrate.AuthorizationDetail{}, fmt.Errorf("a %s detail: %w", typ, err)
	}
	return d, nil
}

// checkDetails holds details to what a request may ask: at most 8, each
// naming what its type names and nothing another type does, none twice.
// A package is `<authority>/<package>` at version 1 or later; an account is
// a kind named in full outside the substrate's own packages, a record id
// other than `default` and at most 1 KB of properties; a webhook is one
// trigger's id.
func checkDetails(details []substrate.AuthorizationDetail) error {
	if len(details) > maxDetails {
		return fmt.Errorf("authorization_details names more than %d entries", maxDetails)
	}
	seen := map[string]bool{}
	for i, d := range details {
		key, err := checkDetail(d)
		if err != nil {
			return fmt.Errorf("authorization_details[%d]: %w", i, err)
		}
		if seen[key] {
			return fmt.Errorf("authorization_details[%d] repeats an earlier entry, %s", i, key)
		}
		seen[key] = true
	}
	return nil
}

// checkDetail checks one entry and answers what makes two entries the same.
func checkDetail(d substrate.AuthorizationDetail) (string, error) {
	if d.URL != "" {
		return "", errors.New("url is the token response's to answer, never a request's to send")
	}
	switch d.Type {
	case substrate.DetailPackage:
		if d.Kind != "" || d.ID != "" || d.Properties != nil || d.Trigger != "" || hasAgentMembers(d) {
			return "", errors.New("a package detail names its package and version alone")
		}
		authority, pkg := vocabulary.SplitPackageRef(d.Package)
		if authority == "" || !vocabulary.ValidAuthority(authority) || !vocabulary.ValidPackage(pkg) {
			return "", fmt.Errorf("package %q is not a package id, <authority>/<package>", d.Package)
		}
		if d.Version < 1 {
			return "", fmt.Errorf("package %s names no version: the least one it needs, 1 or later", d.Package)
		}
		return "package " + d.Package, nil
	case substrate.DetailAccount:
		if d.Package != "" || d.Version != 0 || d.Trigger != "" || hasAgentMembers(d) {
			return "", errors.New("an account detail names its kind, id and properties alone")
		}
		authority, pkg, name := vocabulary.SplitKindRef(d.Kind)
		if authority == "" || !vocabulary.ValidAuthority(authority) || !vocabulary.ValidPackage(pkg) || !vocabulary.ValidName(name) {
			return "", fmt.Errorf("kind %q is not a kind named in full, <authority>/<package>/<name>", d.Kind)
		}
		// The auth kinds are core's, so this refuses them too.
		if vocabulary.SubstrateStatePackage(vocabulary.PackageRef(authority, pkg)) {
			return "", fmt.Errorf("kind %s is the substrate's own, which no app sets up", d.Kind)
		}
		if !vocabulary.ValidID(d.ID) {
			return "", fmt.Errorf("id %q is not a record id", d.ID)
		}
		// A package's input resolves the record named `default` ahead of
		// every other (engine inputs.go), so an app naming it would choose
		// for every package that reads the kind.
		if d.ID == inputDefaultID {
			return "", fmt.Errorf("id %s is the one a package's inputs resolve first: an app names an account of its own", inputDefaultID)
		}
		raw, err := json.Marshal(d.Properties)
		if err != nil {
			return "", fmt.Errorf("properties: %w", err)
		}
		if len(raw) > maxAccountProperties {
			return "", fmt.Errorf("properties are longer than %d bytes", maxAccountProperties)
		}
		return "account " + vocabulary.RecordPath(d.Kind, d.ID), nil
	case substrate.DetailWebhook:
		if d.Package != "" || d.Version != 0 || d.Kind != "" || d.ID != "" || d.Properties != nil || hasAgentMembers(d) {
			return "", errors.New("a webhook detail names its trigger alone")
		}
		// One path segment of the webhook door, and what `webhook:` names.
		if !vocabulary.ValidTriggerID(d.Trigger) {
			return "", fmt.Errorf("trigger %q is not one trigger's id", d.Trigger)
		}
		return "webhook " + d.Trigger, nil
	case substrate.DetailAgent:
		if d.Package != "" || d.Version != 0 || d.Kind != "" || d.ID != "" || d.Properties != nil || d.Trigger != "" {
			return "", errors.New("an agent detail names its agent, reads, writes and spendCentsPerDay alone")
		}
		if err := checkAgentDetail(d); err != nil {
			return "", err
		}
		return "agent " + d.Agent, nil
	}
	return "", fmt.Errorf("type %q is not package, account, webhook or agent", d.Type)
}

func hasAgentMembers(d substrate.AuthorizationDetail) bool {
	return d.Agent != "" || d.Reads != nil || d.Writes != nil || d.SpendCentsPerDay != 0
}

// checkAgentDetail holds an agent detail to decision 0206: one agent of the
// repository's own authority, `~/<package>/<name>`; reads and writes that
// are each one kind under the effective-permission rule
// (scope.AgentKindProblem), none twice, every write also a read; and a
// daily cap of at least a cent and at most the ceiling. The request must
// also carry `declare:~/<package>`, which the authorization endpoint checks
// where it holds both.
func checkAgentDetail(d substrate.AuthorizationDetail) error {
	const shape = "agent %q is not one agent of your own authority, `~/<package>/<name>`"
	rest, ok := strings.CutPrefix(d.Agent, scope.Home+"/")
	pkg, name, _ := strings.Cut(rest, "/")
	if !ok || !vocabulary.ValidPackage(pkg) || !vocabulary.ValidCamel(name) {
		return fmt.Errorf(shape, d.Agent)
	}
	if len(d.Reads) == 0 {
		return fmt.Errorf("agent %s reads nothing: name the kinds it reads, which its query tool is held to", d.Agent)
	}
	if len(d.Reads) > maxAgentReads {
		return fmt.Errorf("agent %s reads more than %d kinds", d.Agent, maxAgentReads)
	}
	if len(d.Writes) > maxAgentWrites {
		return fmt.Errorf("agent %s writes more than %d kinds", d.Agent, maxAgentWrites)
	}
	for _, list := range []struct {
		verb  string
		kinds []string
	}{{"reads", d.Reads}, {"writes", d.Writes}} {
		seen := map[string]bool{}
		for _, k := range list.kinds {
			if p := scope.AgentKindProblem(k); p != "" {
				return fmt.Errorf("agent %s %s %q, which %s", d.Agent, list.verb, k, p)
			}
			if seen[k] {
				return fmt.Errorf("agent %s %s %s twice", d.Agent, list.verb, k)
			}
			seen[k] = true
		}
	}
	for _, k := range d.Writes {
		if !slices.Contains(d.Reads, k) {
			return fmt.Errorf("agent %s writes %s without reading it: every kind it writes is also one it reads", d.Agent, k)
		}
	}
	if d.SpendCentsPerDay < 1 || d.SpendCentsPerDay > substrate.AppAgentSpendCeilingCents {
		return fmt.Errorf("agent %s: spendCentsPerDay must be from 1 to %d cents", d.Agent, substrate.AppAgentSpendCeilingCents)
	}
	return nil
}

// AgentDeclarePackage is the `declare:` scope an agent detail's request must
// carry: the package of its agent, `~/<package>`.
func AgentDeclarePackage(agent string) string {
	pkg, _, _ := strings.Cut(strings.TrimPrefix(agent, scope.Home+"/"), "/")
	return scope.Home + "/" + pkg
}
