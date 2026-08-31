package e2e

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
)

// The vocabulary-upgrade, bundle-lifecycle and GraphQL cases (orders 400-499).
//
// Two rules keep these off the repository the earlier cases built. The
// vocabulary cases own their OWN authority (`upgrades.e2e.example`), applied
// here and nowhere else, so an upgrade or a refused narrowing can never move
// a shipped declaration. The lifecycle case works a THROWAWAY bundle
// (`notes`, a worked example that needs no network and no credentials): it is
// installed here, disabled, purged, uninstalled and reinstalled, while
// people, scheduling, tasks and calendar are only ever read.
const (
	xvAuthority        = "upgrades.e2e.example"
	xvWidgetKind       = xvAuthority + "/widget"
	xvWidgetCollection = "/api/v1/" + xvAuthority + "/widget"
	// The GraphQL type name the widget kind generates: the authority's FIRST
	// LABEL is the prefix (the one place kind identity is still keyed on it),
	// so a rename of that keying fails here loudly.
	xvWidgetGraphQLType = "Upgrades_Widget"

	// The authority VOC-04 tries to admit with a key the dialect does not
	// know. It must never exist afterwards.
	xvSparkleAuthority = "sparkles.e2e.example"

	xvNotesBundle    = "notes.bundles.substrate.reamde.dev/notes"
	xvNoteCollection = "/api/v1/notes.bundles.substrate.reamde.dev/note"
	xvStatsFunction  = "notes.bundles.substrate.reamde.dev/stats"
	xvNoteKind       = "notes.bundles.substrate.reamde.dev/note"

	xvFirecrawlBundle     = "firecrawl.bundles.substrate.reamde.dev/firecrawl"
	xvFirecrawlConfigKind = "firecrawl.bundles.substrate.reamde.dev/config"
	xvFirecrawlConfigs    = "/api/v1/firecrawl.bundles.substrate.reamde.dev/config"

	xvTemporalTrait = "core.substrate.reamde.dev/temporal"

	xvKindCollection      = "/api/v1/core.substrate.reamde.dev/kind"
	xvAuthorityCollection = "/api/v1/core.substrate.reamde.dev/authority"
	xvBundleCollection    = "/api/v1/core.substrate.reamde.dev/bundle"
	xvTraitCollection     = "/api/v1/core.substrate.reamde.dev/trait"
)

func init() {
	registerCase(400, "VOC-02", "An additive vocabulary upgrade lands",
		"A new authority is applied, a record written under it, and a second apply adds an optional "+
			"property and an enum value: both were refused before the upgrade and both write after it, "+
			"the declarations' versions move, and the record written under the old shape reads unchanged.",
		xvCaseAdditiveUpgrade)
	registerCase(410, "VOC-03", "A narrowing is refused while live records hold the old shape",
		"Retyping a property, removing an enum value a live record uses, adding required to a property "+
			"live records lack, and dropping a property live records carry are each refused with a guard "+
			"naming the property and counting the records; the stored vocabulary does not move.",
		xvCaseNarrowingRefused)
	registerCase(420, "VOC-04", "An unknown declaration key is refused at the door",
		"A declaration carrying a key the dialect does not know is refused with a validation error naming "+
			"the key and its path, and the authority is not created: over the API an unknown key never "+
			"reaches storage, so it never quarantines.",
		xvCaseUnknownDialectKey)
	registerCase(430, "VOC-05", "A kind reference as a record id",
		"The kind collection holds declaration records whose ids ARE kind references; the percent-encoded "+
			"GET answers one and its id round-trips, while the unencoded spelling addresses nothing.",
		xvCaseKindReferenceID)
	registerCase(440, "GQL-02", "The GraphQL schema follows a vocabulary apply",
		"Without a restart, the generated schema answers a records query over the kind VOC-02 applied, and "+
			"introspection shows the type carrying the property VOC-02's upgrade added.",
		xvCaseGraphQLFollowsApply)
	registerCase(450, "BUN-03", "The bundle lifecycle on a throwaway bundle",
		"Install, write a record, disable (the bundle's function refuses invocation while ordinary writes "+
			"of its kinds still land), enable, and the teardown order the guards enforce: purge needs a "+
			"disabled bundle, uninstall needs no live records, and the reinstalled bundle comes back "+
			"disabled because the disable outlives the uninstall.",
		xvCaseBundleLifecycle)
	registerCase(460, "BUN-05", "An up-to-date bundle offers no upgrade",
		"The catalog flags the installed bundles and carries an upgrade preview only where the shipped "+
			"closure has moved past what this repository stored; for a bundle installed from that same "+
			"binary the upgrade field is absent, in the listing and in the item detail.",
		xvCaseNoUpgradeOffered)
	registerCase(470, "BUN-06", "Trait endpoints see through installed kinds",
		"The temporal trait's implementors list the kinds that declare it, across every installed "+
			"authority, and its records endpoint pages records of exactly those kinds; an unknown trait "+
			"implements nothing rather than refusing.",
		xvCaseTraitEndpoints)
	registerCase(480, "BUN-04", "A bundle input resolves: sole, default, then bound",
		"An installed bundle with a declared input reads unresolved and names the missing kind as a setup "+
			"step; one record resolves it as the sole one, a second makes it ambiguous, a record named "+
			"`default` resolves it again, and an explicit bind outranks both.",
		xvCaseBundleInput)
}

// ---- the shapes these cases read ------------------------------------------

// xvBundleStatus mirrors substrate.BundleStatus, narrowed to what the bundle
// cases assert on.
type xvBundleStatus struct {
	ID               string          `json:"id"`
	Installed        bool            `json:"installed"`
	Enabled          bool            `json:"enabled"`
	Inputs           []xvInputStatus `json:"inputs"`
	Setup            []xvSetupItem   `json:"setup"`
	Kinds            int             `json:"kinds"`
	Functions        int             `json:"functions"`
	LiveRecords      int64           `json:"liveRecords"`
	Quarantined      bool            `json:"quarantined"`
	QuarantineReason string          `json:"quarantineReason"`
}

type xvInputStatus struct {
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Record string `json:"record"`
	Via    string `json:"via"`
}

type xvSetupItem struct {
	Code    string `json:"code"`
	Input   string `json:"input"`
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

// xvRefusal is the error envelope every refusal here is asserted against.
type xvRefusal struct {
	Code     string
	Message  string
	Problems []string
}

// xvRefused decodes a refusal body. A refusal whose body is not the published
// error envelope is a failure, not a soft assertion: the code and the message
// are what the case pins.
func (c *C) xvRefused(raw []byte) xvRefusal {
	c.t.Helper()
	var body struct {
		Error struct {
			Code     string   `json:"code"`
			Message  string   `json:"message"`
			Problems []string `json:"problems"`
		} `json:"error"`
	}
	c.requiref(json.Unmarshal(raw, &body) == nil && body.Error.Code != "",
		"a refusal did not carry the error envelope: %s", raw)
	return xvRefusal{Code: body.Error.Code, Message: body.Error.Message, Problems: body.Error.Problems}
}

// ---- the upgrades authority -----------------------------------------------

// xvClosure is the whole upgrades authority in one apply batch: the authority
// header, which carries ONLY its version, and the one widget kind. An apply
// replaces the authority whole, so every version ships every declaration.
func xvClosure(version int, properties map[string]any) []map[string]any {
	return []map[string]any{
		{
			"kind":     "core.substrate.reamde.dev/authority",
			"metadata": map[string]any{"id": xvAuthority},
			"data":     map[string]any{"version": version},
		},
		{
			"kind":     "core.substrate.reamde.dev/kind",
			"metadata": map[string]any{"id": xvWidgetKind},
			"data": map[string]any{
				"authority":       xvAuthority,
				"names":           map[string]any{"singular": "widget"},
				"description":     "A thing the vocabulary cases move: one heading and one size.",
				"displayTemplate": "{name}",
				"properties":      properties,
			},
		},
	}
}

// xvPropertiesV1 and xvPropertiesV2 are the two shapes the upgrade sits
// between. Each call builds a fresh map so a case may narrow a copy.
func xvPropertiesV1() map[string]any {
	return map[string]any{
		"name": map[string]any{"type": "string", "description": "the widget's heading"},
		"size": map[string]any{"type": "enum", "values": []string{"small", "large"}, "description": "how big it is"},
	}
}

func xvPropertiesV2() map[string]any {
	props := xvPropertiesV1()
	props["size"] = map[string]any{
		"type":        "enum",
		"values":      []string{"small", "medium", "large"},
		"description": "how big it is",
	}
	props["color"] = map[string]any{"type": "string", "description": "what color it is, added by the upgrade"}
	return props
}

// xvApply posts one vocabulary batch and returns the status and body, so a
// case asserts the admission or the refusal itself.
func (c *C) xvApply(documents []map[string]any) (int, []byte) {
	c.t.Helper()
	return c.do(http.MethodPost, "/api/v1/vocabulary/apply", map[string]any{"documents": documents}, nil)
}

// xvDeclarationVersion reads the stored version of one declaration record.
// The declaration's own record version and its `version` property move
// together on an apply, and both are asserted where they matter.
func (c *C) xvDeclarationVersion(collection, id string) int64 {
	c.t.Helper()
	rec := c.getRec(collection, id)
	v, ok := rec.Properties["version"].(float64)
	c.requiref(ok, "%s/%s carries no version property: %v", collection, id, rec.Properties)
	return int64(v)
}

// xvCaseAdditiveUpgrade: VOC-02.
func xvCaseAdditiveUpgrade(c *C) {
	status, raw := c.xvApply(xvClosure(1, xvPropertiesV1()))
	c.requiref(status == http.StatusOK, "applying `%s` version 1 answered %d: %s", xvAuthority, status, raw)
	c.requiref(c.xvDeclarationVersion(xvAuthorityCollection, xvAuthority) == 1,
		"the fresh authority did not store version 1")
	c.requiref(c.xvDeclarationVersion(xvKindCollection, xvWidgetKind) == 1,
		"the fresh widget kind did not store version 1")
	c.stepf("applied a new authority `%s` at version 1: one kind `widget`, properties `name` and `size` (small, large)", xvAuthority)

	small := c.putRec(xvWidgetCollection, "xv-widget-small",
		map[string]any{"name": "The small widget", "size": "small"})
	c.requiref(small.prop("size") == "small" && small.Version == 1,
		"the first widget landed wrong: size %q, version %d", small.prop("size"), small.Version)
	c.stepf("wrote widget `%s` under version 1 of the declaration", small.ID)

	// What the upgrade is FOR: both spellings are refused first, so the
	// second apply is what admits them rather than something already true.
	status, raw = c.do(http.MethodPost, xvWidgetCollection,
		map[string]any{"properties": map[string]any{"name": "Too soon", "size": "medium"}}, nil)
	c.requiref(status == http.StatusUnprocessableEntity, "an undeclared enum value answered %d, want 422: %s", status, raw)
	ref := c.xvRefused(raw)
	c.requiref(ref.Code == "validation" && strings.Contains(ref.Message, "small, large"),
		"the refusal of `medium` does not name the declared values: %s", ref.Message)
	status, raw = c.do(http.MethodPost, xvWidgetCollection,
		map[string]any{"properties": map[string]any{"name": "Too soon", "color": "teal"}}, nil)
	c.requiref(status == http.StatusUnprocessableEntity, "an undeclared property answered %d, want 422: %s", status, raw)
	ref = c.xvRefused(raw)
	c.requiref(ref.Code == "validation" && strings.Contains(ref.Message, "color"),
		"the refusal of `color` does not name the property: %s", ref.Message)
	c.stepf("before the upgrade both additions are refused 422: `size: medium` is not a declared value and `color` is not a declared property")

	status, raw = c.xvApply(xvClosure(2, xvPropertiesV2()))
	c.requiref(status == http.StatusOK, "the additive upgrade answered %d, want 200: %s", status, raw)
	c.requiref(c.xvDeclarationVersion(xvAuthorityCollection, xvAuthority) == 2,
		"the authority's stored version did not move to 2")
	kindRec := c.getRec(xvKindCollection, xvWidgetKind)
	c.requiref(kindRec.Version == 2, "the widget declaration record's version is %d, want 2", kindRec.Version)
	c.stepf("applied version 2, adding the optional property `color` and the enum value `medium`: the authority and the kind declaration both moved to version 2")

	// The old record is untouched by the upgrade: same values, same version.
	reread := c.getRec(xvWidgetCollection, "xv-widget-small")
	c.requiref(reread.prop("name") == "The small widget" && reread.prop("size") == "small",
		"the pre-upgrade widget reads back changed: %v", reread.Properties)
	c.requiref(reread.Version == small.Version,
		"the upgrade moved the pre-upgrade record's version from %d to %d", small.Version, reread.Version)
	c.stepf("the widget written under version 1 reads back unchanged at record version %d: an additive upgrade rewrites no rows", reread.Version)

	medium := c.putRec(xvWidgetCollection, "xv-widget-medium",
		map[string]any{"name": "The medium widget", "size": "medium", "color": "teal"})
	c.requiref(medium.prop("size") == "medium" && medium.prop("color") == "teal",
		"the post-upgrade widget landed wrong: %v", medium.Properties)
	c.stepf("wrote widget `%s` using both additions: `size: medium` and `color: teal` now land", medium.ID)
}

// xvCaseNarrowingRefused: VOC-03. Every attempt declares version 3, and none
// of them may leave a trace.
func xvCaseNarrowingRefused(c *C) {
	// Each narrowing is built from the live version-2 shape, so the ONLY
	// difference between an admitted apply and a refused one is the narrowing.
	retype := xvPropertiesV2()
	retype["name"] = map[string]any{"type": "int", "description": "the widget's heading"}

	dropValue := xvPropertiesV2()
	dropValue["size"] = map[string]any{
		"type":        "enum",
		"values":      []string{"medium", "large"},
		"description": "how big it is",
	}

	required := xvPropertiesV2()
	required["color"] = map[string]any{"type": "string", "required": true, "description": "what color it is"}

	dropProperty := xvPropertiesV2()
	delete(dropProperty, "color")

	for _, narrowing := range []struct {
		what     string
		property string
		props    map[string]any
	}{
		{"retyping `name` from string to int", "name", retype},
		{"removing the enum value `small`", "size", dropValue},
		{"making `color` required", "color", required},
		{"dropping `color`", "color", dropProperty},
	} {
		status, raw := c.xvApply(xvClosure(3, narrowing.props))
		c.requiref(status == http.StatusForbidden,
			"%s answered %d, want a 403 guard: %s", narrowing.what, status, raw)
		ref := c.xvRefused(raw)
		c.requiref(ref.Code == "guard", "%s was refused with code %q, want `guard`: %s", narrowing.what, ref.Code, ref.Message)
		c.requiref(strings.Contains(ref.Message, `property "`+narrowing.property+`"`),
			"the refusal of %s does not name the property: %s", narrowing.what, ref.Message)
		c.requiref(strings.Contains(ref.Message, "live records"),
			"the refusal of %s does not count the live records standing in the way: %s", narrowing.what, ref.Message)
		c.stepf("%s was refused 403 `guard`: %s", narrowing.what, ref.Message)
	}

	// Nothing moved: the stored declaration is still version 2, and a record
	// still writes with the enum value the second narrowing tried to remove.
	c.requiref(c.xvDeclarationVersion(xvAuthorityCollection, xvAuthority) == 2,
		"a refused narrowing moved the authority's stored version off 2")
	c.requiref(c.xvDeclarationVersion(xvKindCollection, xvWidgetKind) == 2,
		"a refused narrowing moved the widget declaration's stored version off 2")
	still := c.putRec(xvWidgetCollection, "xv-widget-still-small",
		map[string]any{"name": "Still small", "size": "small"})
	c.requiref(still.prop("size") == "small", "a widget can no longer be written `small` after the refusals")
	c.stepf("the stored vocabulary is untouched: both declarations are still version 2 and `%s` writes `size: small`", still.ID)
}

// xvSparkleClosure is one authority whose gadget kind carries the unknown key
// `sparkles`, either on the property or on the kind document itself.
func xvSparkleClosure(onProperty bool) []map[string]any {
	name := map[string]any{"type": "string", "description": "the heading"}
	data := map[string]any{
		"authority":       xvSparkleAuthority,
		"names":           map[string]any{"singular": "gadget"},
		"description":     "A kind carrying a key the dialect does not know.",
		"displayTemplate": "{name}",
		"properties":      map[string]any{"name": name},
	}
	if onProperty {
		name["sparkles"] = true
	} else {
		data["sparkles"] = true
	}
	return []map[string]any{
		{
			"kind":     "core.substrate.reamde.dev/authority",
			"metadata": map[string]any{"id": xvSparkleAuthority},
			"data":     map[string]any{"version": 1},
		},
		{
			"kind":     "core.substrate.reamde.dev/kind",
			"metadata": map[string]any{"id": xvSparkleAuthority + "/gadget"},
			"data":     data,
		},
	}
}

// xvCaseUnknownDialectKey: VOC-04.
func xvCaseUnknownDialectKey(c *C) {
	status, raw := c.xvApply(xvSparkleClosure(true))
	c.requiref(status == http.StatusUnprocessableEntity,
		"a property carrying `sparkles` answered %d, want 422: %s", status, raw)
	ref := c.xvRefused(raw)
	c.requiref(ref.Code == "validation", "the unknown key was refused with code %q, want `validation`", ref.Code)
	c.requiref(strings.Contains(ref.Message, `unknown key "sparkles"`),
		"the refusal does not name the key: %s", ref.Message)
	c.requiref(len(ref.Problems) == 1 && strings.Contains(ref.Problems[0], "data.properties.name"),
		"the refusal does not locate the key on the property: %v", ref.Problems)
	c.stepf("a property carrying the unknown key `sparkles` was refused 422 `validation`, naming the key and where it sat: %s", ref.Problems[0])

	// The same key one level up, on the kind itself, is refused the same way:
	// the key set is closed at every level, not only inside a property.
	status, raw = c.xvApply(xvSparkleClosure(false))
	c.requiref(status == http.StatusUnprocessableEntity, "a kind carrying `sparkles` answered %d, want 422: %s", status, raw)
	ref = c.xvRefused(raw)
	c.requiref(strings.Contains(ref.Message, `unknown key "sparkles"`), "the refusal does not name the key: %s", ref.Message)
	c.stepf("the same key on the kind document itself is refused the same way: a declaration's key set is closed at every level")

	// Nothing was stored, so there is nothing to quarantine.
	status, raw = c.do(http.MethodGet, xvAuthorityCollection+"/"+url.PathEscape(xvSparkleAuthority), nil, nil)
	c.requiref(status == http.StatusNotFound, "the refused authority exists: GET answered %d: %s", status, raw)
	status, _ = c.do(http.MethodGet, "/api/v1/"+xvSparkleAuthority+"/gadget", nil, nil)
	c.requiref(status == http.StatusNotFound, "the refused kind has a collection: GET answered %d", status)
	c.stepf("nothing was stored: the authority record and the collection both 404. Over the live door an unknown " +
		"declaration key is REFUSED, never quarantined; the quarantine in decision 0020 is the repository-open " +
		"path, where a closure ALREADY STORED meets a binary that does not know one of its keys, which no API call can reach")
}

// xvCaseKindReferenceID: VOC-05.
func xvCaseKindReferenceID(c *C) {
	encoded := xvKindCollection + "/" + url.PathEscape(taskKind)
	c.requiref(strings.Contains(encoded, "%2F"), "url.PathEscape did not percent-encode the reference's slash: %s", encoded)

	var declaration record
	status, raw := c.do(http.MethodGet, encoded, nil, &declaration)
	c.requiref(status == http.StatusOK, "the percent-encoded kind GET answered %d: %s", status, raw)
	c.requiref(declaration.ID == taskKind,
		"the declaration's id is %q, want the kind reference %q", declaration.ID, taskKind)
	c.requiref(declaration.Kind == "core.substrate.reamde.dev/kind",
		"the declaration's kind is %q", declaration.Kind)
	c.requiref(declaration.Properties["properties"] != nil && declaration.prop("authority") == "tasks.substrate.reamde.dev",
		"the declaration record does not carry the kind's definition: %v", declaration.Properties)
	c.stepf("`GET %s` answered the declaration of `%s`: authority `%s`, its declared properties on the record",
		encoded, declaration.ID, declaration.prop("authority"))

	// The id round-trips: re-encoding what came back addresses the same
	// record, so a client can page the collection and follow its own ids.
	var again record
	status, _ = c.do(http.MethodGet, xvKindCollection+"/"+url.PathEscape(declaration.ID), nil, &again)
	c.requiref(status == http.StatusOK && again.ID == declaration.ID,
		"re-encoding the id the server returned does not address the same record: %d, %q", status, again.ID)

	// The unencoded spelling is a four-segment path, which names no route.
	status, _ = c.do(http.MethodGet, xvKindCollection+"/"+taskKind, nil, nil)
	c.requiref(status == http.StatusNotFound,
		"the unencoded kind reference answered %d, want 404: the slash must be encoded, never a path segment", status)
	c.stepf("the id round-trips through `url.PathEscape`, and the unencoded spelling `%s/%s` is a 404: the reference's slash is data, not a path separator", xvKindCollection, taskKind)

	// The collection holds them all, VOC-02's own kind among them.
	var page struct {
		Records []record `json:"records"`
	}
	status, raw = c.do(http.MethodGet, xvKindCollection+"?first=200", nil, &page)
	c.requiref(status == http.StatusOK, "listing the kind collection answered %d: %s", status, raw)
	found := map[string]bool{}
	for _, rec := range page.Records {
		found[rec.ID] = true
	}
	c.requiref(found[taskKind] && found[xvWidgetKind],
		"the kind collection is missing %s or %s among its %d records", taskKind, xvWidgetKind, len(page.Records))
	c.stepf("the kind collection lists %d declarations, every id a kind reference, `%s` and `%s` among them",
		len(page.Records), taskKind, xvWidgetKind)
}

// xvCaseGraphQLFollowsApply: GQL-02. The schema is generated per repository
// from the live registry, so a kind applied minutes ago is queryable in the
// same process.
func xvCaseGraphQLFollowsApply(c *C) {
	query := `{ records(filter: {kinds: ["` + xvWidgetKind + `"]}, first: 20) { nodes { id kind properties } } }`
	var answer struct {
		Data struct {
			Records struct {
				Nodes []struct {
					ID         string         `json:"id"`
					Kind       string         `json:"kind"`
					Properties map[string]any `json:"properties"`
				} `json:"nodes"`
			} `json:"records"`
		} `json:"data"`
	}
	status, raw := c.do(http.MethodPost, "/api/v1/graphql", map[string]any{"query": query}, &answer)
	c.requiref(status == http.StatusOK && !strings.Contains(string(raw), `"errors"`),
		"the widget query answered %d: %s", status, raw)
	byID := map[string]map[string]any{}
	for _, node := range answer.Data.Records.Nodes {
		c.requiref(node.Kind == xvWidgetKind, "the query answered a %s node", node.Kind)
		byID[node.ID] = node.Properties
	}
	c.requiref(byID["xv-widget-small"] != nil && byID["xv-widget-medium"] != nil,
		"GraphQL does not see VOC-02's widgets: %s", raw)
	c.requiref(byID["xv-widget-medium"]["color"] == "teal",
		"the medium widget's `color` did not come back through GraphQL: %v", byID["xv-widget-medium"])
	c.stepf("`POST /api/v1/graphql` answered %d widgets of `%s` with no restart between the apply and the query",
		len(answer.Data.Records.Nodes), xvWidgetKind)

	// Introspection carries the upgrade too: `color` is a field on the
	// generated type, so the schema followed the SECOND apply, not just the
	// first.
	var introspection struct {
		Data struct {
			Type *struct {
				Name   string `json:"name"`
				Fields []struct {
					Name string `json:"name"`
				} `json:"fields"`
			} `json:"__type"`
		} `json:"data"`
	}
	status, raw = c.do(http.MethodPost, "/api/v1/graphql",
		map[string]any{"query": `{ __type(name: "` + xvWidgetGraphQLType + `") { name fields { name } } }`}, &introspection)
	c.requiref(status == http.StatusOK && introspection.Data.Type != nil,
		"introspection found no type %q: %d %s", xvWidgetGraphQLType, status, raw)
	fields := map[string]bool{}
	for _, f := range introspection.Data.Type.Fields {
		fields[f.Name] = true
	}
	c.requiref(fields["name"] && fields["size"] && fields["color"],
		"the generated type is missing a declared property: %s", raw)
	c.stepf("introspection answers the type `%s` with `name`, `size` and `color` as fields: the schema followed the upgrade, not only the first apply",
		introspection.Data.Type.Name)
}

// ---- the bundle lifecycle --------------------------------------------------

// xvBundleRecord is the bundle record's path: the lifecycle is a PATCH of the
// record, not a verb.
func xvBundleRecord(id string) string {
	return xvBundleCollection + "/" + url.PathEscape(id)
}

// xvStatus reads one bundle's computed status.
func (c *C) xvStatus(id string) xvBundleStatus {
	c.t.Helper()
	var st xvBundleStatus
	status, raw := c.do(http.MethodGet, xvBundleRecord(id)+"/status", nil, &st)
	c.requiref(status == http.StatusOK, "the status of %s answered %d: %s", id, status, raw)
	return st
}

// xvLifecycle patches one lifecycle state and returns the answer.
func (c *C) xvLifecycle(id, property string, value any) (int, []byte) {
	c.t.Helper()
	return c.do(http.MethodPatch, xvBundleRecord(id),
		map[string]any{"properties": map[string]any{property: value}}, nil)
}

// xvInstall installs one catalog bundle and returns the status it answers.
func (c *C) xvInstall(id string) xvBundleStatus {
	c.t.Helper()
	var st xvBundleStatus
	status, raw := c.do(http.MethodPost, "/api/v1/catalog/"+url.PathEscape(id)+"/install", nil, &st)
	c.requiref(status == http.StatusOK, "installing %s answered %d: %s", id, status, raw)
	c.requiref(st.Installed, "the install of %s did not end installed: %s", id, raw)
	return st
}

// xvCallStats invokes the notes bundle's pure function.
func (c *C) xvCallStats(text string) (int, []byte) {
	c.t.Helper()
	return c.do(http.MethodPost,
		"/api/v1/core.substrate.reamde.dev/function/"+url.PathEscape(xvStatsFunction)+"/call",
		map[string]any{"input": map[string]any{"text": text}}, nil)
}

// xvCaseBundleLifecycle: BUN-03, on the notes bundle. Nothing the earlier
// cases built is touched: notes ships its own authority, needs no network and
// no credential, and this case leaves it installed and enabled.
func xvCaseBundleLifecycle(c *C) {
	st := c.xvInstall(xvNotesBundle)
	c.requiref(st.ID == xvNotesBundle, "the install answered the status of %q", st.ID)
	c.requiref(st.Enabled, "the freshly installed bundle is not enabled: %+v", st)
	c.requiref(!st.Quarantined, "the freshly installed bundle is quarantined: %s", st.QuarantineReason)
	c.stepf("installed the throwaway bundle `%s`: %d kinds, %d functions, enabled and not quarantined", xvNotesBundle, st.Kinds, st.Functions)

	note := c.putRec(xvNoteCollection, "xv-note-one", map[string]any{
		"text": "The lifecycle case wrote this note.", "noteTitle": "Lifecycle", "words": 6, "characters": 35,
	})
	c.requiref(note.prop("noteTitle") == "Lifecycle", "the note landed wrong: %v", note.Properties)

	status, raw := c.xvCallStats("one two three")
	c.requiref(status == http.StatusOK, "calling %s answered %d: %s", xvStatsFunction, status, raw)
	var out struct {
		Output struct {
			Words      float64 `json:"words"`
			Characters float64 `json:"characters"`
		} `json:"output"`
	}
	c.requiref(json.Unmarshal(raw, &out) == nil && out.Output.Words == 3 && out.Output.Characters > 0,
		"the function answered the wrong output: %s", raw)
	c.stepf("wrote note `%s` and the bundle's function `%s` runs: 3 words, %d characters", note.ID, xvStatsFunction, int(out.Output.Characters))

	// Disable stops EXECUTION, not data.
	status, raw = c.xvLifecycle(xvNotesBundle, "disabled", true)
	c.requiref(status == http.StatusOK, "disabling answered %d: %s", status, raw)
	st = c.xvStatus(xvNotesBundle)
	c.requiref(st.Installed && !st.Enabled, "after the disable the status is installed=%t enabled=%t", st.Installed, st.Enabled)

	status, raw = c.xvCallStats("one two three")
	c.requiref(status == http.StatusForbidden, "the function of a disabled bundle answered %d, want 403: %s", status, raw)
	ref := c.xvRefused(raw)
	c.requiref(ref.Code == "guard" && strings.Contains(ref.Message, "disabled") && strings.Contains(ref.Message, xvStatsFunction),
		"the refusal does not name the disabled bundle and the function: %s", ref.Message)
	quiet := c.putRec(xvNoteCollection, "xv-note-two", map[string]any{"text": "Written while disabled."})
	var listed struct {
		Records []record `json:"records"`
	}
	status, _ = c.do(http.MethodGet, xvNoteCollection, nil, &listed)
	c.requiref(status == http.StatusOK && len(listed.Records) >= 2,
		"the disabled bundle's records stopped listing: %d, status %d", len(listed.Records), status)
	c.stepf("disabled: the function refuses with 403 `guard` (%q), while `%s` still writes and both notes still list. "+
		"A disable stops execution, not data", ref.Message, quiet.ID)

	status, raw = c.xvLifecycle(xvNotesBundle, "disabled", nil)
	c.requiref(status == http.StatusOK, "enabling answered %d: %s", status, raw)
	c.requiref(c.xvStatus(xvNotesBundle).Enabled, "the bundle did not come back enabled")
	status, _ = c.xvCallStats("one two three")
	c.requiref(status == http.StatusOK, "the function still refuses after the enable: %d", status)
	c.stepf("`disabled: null` enables again (the PATCH-null-deletes convention) and the function runs")

	// The teardown order the guards enforce, both refusals first.
	status, raw = c.xvLifecycle(xvNotesBundle, "uninstalled", true)
	c.requiref(status == http.StatusForbidden, "uninstalling over live records answered %d, want 403: %s", status, raw)
	ref = c.xvRefused(raw)
	c.requiref(strings.Contains(ref.Message, xvNoteKind) && strings.Contains(ref.Message, "live records"),
		"the refusal does not name the kind whose records stand in the way: %s", ref.Message)
	c.stepf("uninstall while records live is refused 403 `guard`: %s", ref.Message)

	status, raw = c.xvLifecycle(xvNotesBundle, "purging", true)
	c.requiref(status == http.StatusForbidden, "purging a live bundle answered %d, want 403: %s", status, raw)
	ref = c.xvRefused(raw)
	c.requiref(strings.Contains(ref.Message, "disable"), "the refusal does not name the disable a purge needs: %s", ref.Message)
	c.stepf("purge while the bundle is live is refused 403 `guard`: %s", ref.Message)

	status, raw = c.xvLifecycle(xvNotesBundle, "disabled", true)
	c.requiref(status == http.StatusOK, "disabling before the purge answered %d: %s", status, raw)
	var purge struct {
		Purged int `json:"purged"`
	}
	status, raw = c.do(http.MethodPatch, xvBundleRecord(xvNotesBundle),
		map[string]any{"properties": map[string]any{"purging": true}}, &purge)
	c.requiref(status == http.StatusOK, "purging answered %d: %s", status, raw)
	c.requiref(purge.Purged == 2, "the purge counted %d records; this case wrote exactly 2, so any other count purged the wrong rows", purge.Purged)
	listed.Records = nil
	status, _ = c.do(http.MethodGet, xvNoteCollection, nil, &listed)
	c.requiref(status == http.StatusOK && len(listed.Records) == 0,
		"the collection still lists %d records after the purge", len(listed.Records))
	var tombstone record
	status, _ = c.do(http.MethodGet, xvNoteCollection+"/xv-note-one", nil, &tombstone)
	c.requiref(status == http.StatusOK && tombstone.DeletedAt != "",
		"a purged record should still GET as a tombstone; answered %d with deletedAt %q", status, tombstone.DeletedAt)
	c.stepf("disabled, then purged: `{\"purged\":%d}`, the collection lists nothing, and `%s` still GETs as a tombstone. "+
		"A purge tombstones the data, it does not erase the history", purge.Purged, tombstone.ID)

	var uninstalled struct {
		Uninstalled bool `json:"uninstalled"`
	}
	status, raw = c.do(http.MethodPatch, xvBundleRecord(xvNotesBundle),
		map[string]any{"properties": map[string]any{"uninstalled": true}}, &uninstalled)
	c.requiref(status == http.StatusOK && uninstalled.Uninstalled, "uninstalling answered %d: %s", status, raw)
	status, _ = c.do(http.MethodGet, xvNoteCollection, nil, nil)
	c.requiref(status == http.StatusNotFound, "the collection answered %d after the uninstall, want 404", status)
	status, _ = c.do(http.MethodGet, xvBundleRecord(xvNotesBundle)+"/status", nil, nil)
	c.requiref(status == http.StatusNotFound, "the bundle status answered %d after the uninstall, want 404", status)
	status, _ = c.xvCallStats("one two three")
	c.requiref(status == http.StatusNotFound, "the bundle's function answered %d after the uninstall, want 404", status)
	c.stepf("uninstalled: the collection, the bundle status and the function are all 404. An uninstalled bundle has no status, it simply stops being listed")

	st = c.xvInstall(xvNotesBundle)
	c.requiref(!st.Enabled,
		"the reinstalled bundle came back enabled; this run's uninstall happened while it was disabled")
	// A `null` records array leaves an already-decoded slice alone, so the
	// emptiness assertion below needs the reset.
	listed.Records = nil
	status, _ = c.do(http.MethodGet, xvNoteCollection, nil, &listed)
	c.requiref(status == http.StatusOK && len(listed.Records) == 0,
		"the collection came back from the reinstall with %d records, status %d", len(listed.Records), status)
	c.stepf("reinstalled from the catalog: the closure is back and the collection is empty, and the bundle comes back DISABLED. " +
		"The disable outlives the uninstall")

	status, raw = c.xvLifecycle(xvNotesBundle, "disabled", false)
	c.requiref(status == http.StatusOK, "re-enabling answered %d: %s", status, raw)
	c.requiref(c.xvStatus(xvNotesBundle).Enabled, "the bundle is still disabled after `disabled: false`")

	// The PATCH carries ONE lifecycle state and takes no precondition.
	status, raw = c.do(http.MethodPatch, xvBundleRecord(xvNotesBundle),
		map[string]any{"properties": map[string]any{"disabled": true, "purging": true}}, nil)
	c.requiref(status == http.StatusBadRequest, "a two-state PATCH answered %d, want 400: %s", status, raw)
	status, raw = c.do(http.MethodPatch, xvBundleRecord(xvNotesBundle),
		map[string]any{"properties": map[string]any{"disabled": true}, "ifVersion": 1}, nil)
	c.requiref(status == http.StatusBadRequest, "a PATCH with ifVersion answered %d, want 400: %s", status, raw)
	c.requiref(strings.Contains(string(raw), "ifVersion"), "the refusal does not name ifVersion: %s", raw)
	c.requiref(c.xvStatus(xvNotesBundle).Enabled, "a refused lifecycle PATCH disabled the bundle anyway")
	c.stepf("`disabled: false` enables too; two states in one PATCH is a 400, and `ifVersion` is refused rather than ignored, because the transition takes no compare-and-set. The bundle is left installed and enabled")
}

// xvCatalogItems reads the catalog as raw maps, so a case can assert a field
// is ABSENT rather than merely zero.
func (c *C) xvCatalogItems() map[string]map[string]any {
	c.t.Helper()
	var listing struct {
		Items []map[string]any `json:"items"`
	}
	status, raw := c.do(http.MethodGet, "/api/v1/catalog", nil, &listing)
	c.requiref(status == http.StatusOK, "the catalog answered %d: %s", status, raw)
	items := map[string]map[string]any{}
	for _, item := range listing.Items {
		id, _ := item["id"].(string)
		items[id] = item
	}
	return items
}

// xvCaseNoUpgradeOffered: BUN-05.
func xvCaseNoUpgradeOffered(c *C) {
	items := c.xvCatalogItems()
	tasks := items[tasksBundleID]
	c.requiref(tasks != nil, "the catalog does not list %s", tasksBundleID)
	installed, _ := tasks["installed"].(bool)
	c.requiref(installed, "%s is not installed; the earlier cases install it", tasksBundleID)
	_, offered := tasks["upgrade"]
	c.requiref(!offered, "the catalog offers an upgrade for a bundle installed from this same binary: %v", tasks["upgrade"])
	c.stepf("`%s` lists installed=true with NO `upgrade` field: the shipped closure has not moved past what this repository stored, so there is nothing to offer",
		tasksBundleID)

	installedCount, offers := 0, []string{}
	for id, item := range items {
		if on, _ := item["installed"].(bool); !on {
			// An uninstalled bundle never carries a preview: an upgrade of
			// something absent means nothing.
			_, has := item["upgrade"]
			c.requiref(!has, "the catalog offers an upgrade for the uninstalled %s", id)
			continue
		}
		installedCount++
		if _, has := item["upgrade"]; has {
			offers = append(offers, id)
		}
	}
	c.requiref(len(offers) == 0, "the catalog offers upgrades for bundles this binary just installed: %v", offers)
	c.stepf("the same holds across the listing: %d installed bundles, %d upgrade offers, and no offer on any uninstalled entry", installedCount, len(offers))

	var detail map[string]any
	status, raw := c.do(http.MethodGet, "/api/v1/catalog/"+url.PathEscape(tasksBundleID), nil, &detail)
	c.requiref(status == http.StatusOK, "the catalog item answered %d: %s", status, raw)
	_, offered = detail["upgrade"]
	c.requiref(!offered, "the item detail offers an upgrade: %v", detail["upgrade"])
	version, _ := detail["version"].(float64)
	c.stepf("the item detail agrees: `%s` is at version %d with no `upgrade` field. The preview is attached only when a re-install would move a declaration",
		tasksBundleID, int64(version))
}

// xvCaseTraitEndpoints: BUN-06.
func xvCaseTraitEndpoints(c *C) {
	var implementors struct {
		Items []struct {
			Identity  string `json:"identity"`
			Name      string `json:"name"`
			Authority string `json:"authority"`
		} `json:"items"`
	}
	path := xvTraitCollection + "/" + url.PathEscape(xvTemporalTrait) + "/implementors"
	status, raw := c.do(http.MethodGet, path, nil, &implementors)
	c.requiref(status == http.StatusOK, "the implementors answered %d: %s", status, raw)
	kinds := map[string]bool{}
	authorities := map[string]bool{}
	for _, item := range implementors.Items {
		kinds[item.Identity] = true
		authorities[item.Authority] = true
	}
	c.requiref(kinds[taskKind] && kinds[eventKind],
		"the temporal implementors are missing %s or %s: %v", taskKind, eventKind, kinds)
	c.requiref(len(authorities) > 1, "the implementors come from one authority only: %v", authorities)
	c.stepf("`%s` lists %d kinds implementing `%s` across %d authorities, `%s` and `%s` among them",
		path, len(implementors.Items), xvTemporalTrait, len(authorities), taskKind, eventKind)

	// The records endpoint pages the records of exactly those kinds.
	records := xvTraitCollection + "/" + url.PathEscape(xvTemporalTrait) + "/records"
	var first struct {
		Records []record `json:"records"`
		Cursor  string   `json:"cursor"`
	}
	status, raw = c.do(http.MethodGet, records+"?first=2", nil, &first)
	c.requiref(status == http.StatusOK, "the trait records answered %d: %s", status, raw)
	c.requiref(len(first.Records) == 2, "`first=2` answered %d records", len(first.Records))
	c.requiref(first.Cursor != "", "a full page carries no cursor to page on: %s", raw)
	seen := map[string]bool{}
	for _, rec := range first.Records {
		c.requiref(kinds[rec.Kind], "the trait records answered a %s record, which implements no temporal", rec.Kind)
		seen[rec.Kind+"/"+rec.ID] = true
	}

	var second struct {
		Records []record `json:"records"`
	}
	status, raw = c.do(http.MethodGet, records+"?first=2&after="+url.QueryEscape(first.Cursor), nil, &second)
	c.requiref(status == http.StatusOK, "the second page answered %d: %s", status, raw)
	c.requiref(len(second.Records) > 0, "the second page is empty; the story repository holds more than two temporal records")
	for _, rec := range second.Records {
		c.requiref(kinds[rec.Kind], "the second page answered a %s record, which implements no temporal", rec.Kind)
		c.requiref(!seen[rec.Kind+"/"+rec.ID], "the second page repeats %s from the first", rec.ID)
	}
	c.stepf("`%s?first=2` answered 2 records of implementing kinds with a cursor, and `after=` the cursor answered %d more with no repeats",
		records, len(second.Records))

	// An unknown trait implements nothing; it is not an error.
	implementors.Items = nil
	status, raw = c.do(http.MethodGet, xvTraitCollection+"/"+url.PathEscape("core.substrate.reamde.dev/nosuchtrait")+"/implementors", nil, &implementors)
	c.requiref(status == http.StatusOK && len(implementors.Items) == 0,
		"an unknown trait answered %d with %d implementors: %s", status, len(implementors.Items), raw)
	c.stepf("an unknown trait answers 200 with an empty list: nothing implements it, which is an answer rather than a refusal")
}

// xvInput returns one named input's resolution and the setup steps standing
// against it, both looked up by name: a status may carry setup items about
// other things entirely (an agent's missing llmprovider row), and those are
// not this input's business.
func xvInput(st xvBundleStatus, name string) (xvInputStatus, []xvSetupItem) {
	var found xvInputStatus
	for _, in := range st.Inputs {
		if in.Name == name {
			found = in
		}
	}
	steps := []xvSetupItem{}
	for _, item := range st.Setup {
		if item.Input == name {
			steps = append(steps, item)
		}
	}
	return found, steps
}

// xvCaseBundleInput: BUN-04, on firecrawl, the one shipped bundle whose input
// is bindable without an OAuth consent: a plain config record carrying an API
// key, no host flow, no account.
func xvCaseBundleInput(c *C) {
	st := c.xvInstall(xvFirecrawlBundle)
	in, steps := xvInput(st, "connector")
	c.requiref(in.Name == "connector" && in.Kind == xvFirecrawlConfigKind,
		"the installed bundle declares no `connector` input of %s: %+v", xvFirecrawlConfigKind, st.Inputs)
	c.requiref(in.Record == "" && in.Via == "", "a freshly installed bundle's input already resolves to %q", in.Record)
	c.requiref(len(steps) == 1 && steps[0].Code == "missing",
		"the unresolved input's setup steps are %+v, want exactly one `missing`", steps)
	c.requiref(steps[0].Kind == xvFirecrawlConfigKind,
		"the setup step names kind %q, want the input's own kind", steps[0].Kind)
	c.stepf("installed `%s`: its declared input `connector` reads unresolved, and the setup step names the kind that would clear it: %q",
		xvFirecrawlBundle, steps[0].Message)

	// One record, and the input resolves as the sole one.
	sole := c.putRec(xvFirecrawlConfigs, "xv-firecrawl-key",
		map[string]any{"apiKey": "fc-e2e-not-a-real-key", "baseUrl": "https://api.firecrawl.dev"})
	c.requiref(sole.prop("apiKey") == "<redacted>",
		"the config's secret read back as %q; a secret property is never readable over the API", sole.prop("apiKey"))
	in, steps = xvInput(c.xvStatus(xvFirecrawlBundle), "connector")
	c.requiref(in.Record == sole.ID && in.Via == "sole",
		"one record resolved as record=%q via=%q, want the sole one", in.Record, in.Via)
	c.requiref(len(steps) == 0, "a resolved input still carries setup steps: %+v", steps)
	c.stepf("wrote one `%s` record `%s` (its `apiKey` reads back `<redacted>`): the input resolves via `sole`", xvFirecrawlConfigKind, sole.ID)

	// A second record makes the choice ambiguous: the substrate refuses to
	// tie-break, it does not pick one.
	second := c.putRec(xvFirecrawlConfigs, "xv-firecrawl-spare", map[string]any{"apiKey": "fc-e2e-spare"})
	in, steps = xvInput(c.xvStatus(xvFirecrawlBundle), "connector")
	c.requiref(in.Record == "" && in.Via == "", "with two records the input still resolves to %q via %q", in.Record, in.Via)
	c.requiref(len(steps) == 1 && steps[0].Code == "ambiguous",
		"two records give setup steps %+v, want exactly one `ambiguous`", steps)
	c.stepf("a second record `%s` makes it ambiguous: an unbound input with two candidates resolves to nothing rather than guessing", second.ID)

	// The id `default` is the next step in the order.
	byDefault := c.putRec(xvFirecrawlConfigs, "default", map[string]any{"apiKey": "fc-e2e-default"})
	in, _ = xvInput(c.xvStatus(xvFirecrawlBundle), "connector")
	c.requiref(in.Record == byDefault.ID && in.Via == "default",
		"the record named `default` resolved as record=%q via=%q", in.Record, in.Via)
	c.stepf("a record at the id `default` resolves the input via `default`, ahead of the two unnamed candidates")

	// A bind outranks it.
	var bound xvBundleStatus
	status, raw := c.do(http.MethodPost, xvBundleRecord(xvFirecrawlBundle)+"/bind",
		map[string]any{"input": "connector", "record": second.ID}, &bound)
	c.requiref(status == http.StatusOK, "binding answered %d: %s", status, raw)
	in, steps = xvInput(bound, "connector")
	c.requiref(in.Record == second.ID && in.Via == "bound",
		"after the bind the input resolves to record=%q via=%q, want %s via bound", in.Record, in.Via, second.ID)
	c.requiref(len(steps) == 0, "a bound input still carries setup steps: %+v", steps)
	c.stepf("`POST %s/bind {\"input\":\"connector\",\"record\":\"%s\"}` answers the refreshed status: via `bound`, outranking the `default` record. The order is bound, default, sole",
		xvBundleRecord(xvFirecrawlBundle), second.ID)

	// The refusals around the verb.
	status, raw = c.do(http.MethodPost, xvBundleRecord(xvFirecrawlBundle)+"/bind",
		map[string]any{"input": "nosuchinput", "record": second.ID}, nil)
	c.requiref(status == http.StatusNotFound, "binding an undeclared input answered %d, want 404: %s", status, raw)
	ref := c.xvRefused(raw)
	c.requiref(strings.Contains(ref.Message, "nosuchinput"), "the refusal does not name the input: %s", ref.Message)
	status, raw = c.do(http.MethodPost, xvBundleRecord(xvFirecrawlBundle)+"/bind",
		map[string]any{"input": "connector", "record": "xv-no-such-config"}, nil)
	c.requiref(status == http.StatusNotFound, "binding to a record that does not exist answered %d, want 404: %s", status, raw)
	status, raw = c.do(http.MethodPost, xvBundleRecord(xvFirecrawlBundle)+"/bind",
		map[string]any{"input": "", "record": second.ID}, nil)
	c.requiref(status == http.StatusBadRequest, "binding with no input named answered %d, want 400: %s", status, raw)
	in, _ = xvInput(c.xvStatus(xvFirecrawlBundle), "connector")
	c.requiref(in.Record == second.ID && in.Via == "bound", "a refused bind moved the resolution to %q via %q", in.Record, in.Via)
	c.stepf("an undeclared input name is a 404 naming it, a bind to a record that does not exist is a 404, and a bind naming no input is a 400; none of them moves the standing bind")

	final := c.xvStatus(xvFirecrawlBundle)
	c.requiref(final.LiveRecords >= 3, "the bundle counts %d live records, want the three config records this case wrote", final.LiveRecords)
	c.stepf("the bundle is left installed, enabled and bound, counting %d live records of its own authority", final.LiveRecords)

	// The whole flow ran without an OAuth consent: firecrawl's input is a
	// plain config record. A bundle whose input is an OAuth CLIENT (google,
	// github, linear, whoop) binds the same way, but exercising what the
	// binding then unlocks needs a provider consent, which is OAU-01's
	// business and not this case's.
	c.stepf("SKIPPED here: the same bind against an oauth2 bundle's `client` input. Binding it is the identical call, " +
		"but nothing downstream of it moves without a provider consent, which the OAuth cases own")
}
