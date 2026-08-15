// Conformance 3, hand-written like its siblings: Decode(Properties(x)) == x for
// a fixture per kind.
//
// The distinction it exists for is ABSENCE. A nil pointer must come back nil and
// not as an empty string, an empty slice must come back empty and not nil, and a
// null in the map must come back as absence — each of those is a stored value
// nobody wrote, which is the silent bug pointers were chosen to prevent. Every
// kind gets two passes: one fixture with nothing set, and one with as much set
// as its declaration admits.
package corekinds_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/geoah/substrate/internal/corekinds"
	"github.com/geoah/substrate/internal/vocabulary"
)

// properties is what every generated struct offers: its properties map.
type properties[T any] interface {
	*T
	Encode() map[string]any
}

// roundTrip is the whole assertion: encode, decode, compare. A fixture that
// survives it proves the generated pair are inverses over exactly the shapes the
// declaration admits.
func roundTrip[T any, PT properties[T]](t *testing.T, name string, fixture PT, decode func(map[string]any) (*T, []corekinds.Problem)) {
	t.Helper()
	t.Run(name, func(t *testing.T) {
		props := fixture.Encode()
		got, problems := decode(props)
		if len(problems) > 0 {
			t.Fatalf("decoding %v: %v", props, problems)
		}
		if !reflect.DeepEqual(got, (*T)(fixture)) {
			t.Errorf("round trip differs:\n got %+v\nwant %+v", got, (*T)(fixture))
		}
	})
}

func str(s string) *string   { return &s }
func i64(n int64) *int64     { return &n }
func f64(f float64) *float64 { return &f }
func boolean(b bool) *bool   { return &b }
func secret(s string) *corekinds.SecretRef {
	r := corekinds.SecretRef(s)
	return &r
}

func refPath(s string) *corekinds.ReferencePath {
	r := corekinds.ReferencePath(s)
	return &r
}

// The one digest shape a decoder admits: 64 lowercase hex.
const testDigest = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// The one datetime shape: stored verbatim, so the fixture is what comes back.
const testInstant = "2026-08-14T09:41:00.123456789Z"

// TestRoundTripEmpty is the absence half: a struct with nothing set encodes to
// an empty map and decodes back to nothing set. A generated field that
// materialized its zero would fail here, on every kind at once.
func TestRoundTripEmpty(t *testing.T) {
	roundTrip(t, "actor", &corekinds.Actor{}, corekinds.DecodeActor)
	roundTrip(t, "agent", &corekinds.Agent{}, corekinds.DecodeAgent)
	roundTrip(t, "authority", &corekinds.Authority{}, corekinds.DecodeAuthority)
	roundTrip(t, "blob", &corekinds.Blob{}, corekinds.DecodeBlob)
	roundTrip(t, "bundle", &corekinds.Bundle{}, corekinds.DecodeBundle)
	roundTrip(t, "credential", &corekinds.Credential{}, corekinds.DecodeCredential)
	roundTrip(t, "function", &corekinds.Function{}, corekinds.DecodeFunction)
	roundTrip(t, "kind", &corekinds.Kind{}, corekinds.DecodeKind)
	roundTrip(t, "llmprovider", &corekinds.LLMProvider{}, corekinds.DecodeLLMProvider)
	roundTrip(t, "propertytype", &corekinds.PropertyType{}, corekinds.DecodePropertyType)
	roundTrip(t, "recordmapping", &corekinds.RecordMapping{}, corekinds.DecodeRecordMapping)
	roundTrip(t, "recordmerge", &corekinds.RecordMerge{}, corekinds.DecodeRecordMerge)
	roundTrip(t, "recordmergerequest", &corekinds.RecordMergeRequest{}, corekinds.DecodeRecordMergeRequest)
	roundTrip(t, "recordpatchrequest", &corekinds.RecordPatchRequest{}, corekinds.DecodeRecordPatchRequest)
	roundTrip(t, "recordsplit", &corekinds.RecordSplit{}, corekinds.DecodeRecordSplit)
	roundTrip(t, "recoverykey", &corekinds.RecoveryKey{}, corekinds.DecodeRecoveryKey)
	roundTrip(t, "repository", &corekinds.Repository{}, corekinds.DecodeRepository)
	roundTrip(t, "run", &corekinds.Run{}, corekinds.DecodeRun)
	roundTrip(t, "token", &corekinds.Token{}, corekinds.DecodeToken)
	roundTrip(t, "trait", &corekinds.Trait{}, corekinds.DecodeTrait)
	roundTrip(t, "trigger", &corekinds.Trigger{}, corekinds.DecodeTrigger)
	// llmmessage and llmthread each declare a REQUIRED reference, and their empty
	// fixture is empty all the same: the write path does not enforce
	// `required:`, so a stored row can lack one and these types have to hold it.
	// TestRequiredIsMetadata is where that requirement is answered.
	roundTrip(t, "llmmessage", &corekinds.LLMMessage{}, corekinds.DecodeLLMMessage)
	roundTrip(t, "llmthread", &corekinds.LLMThread{}, corekinds.DecodeLLMThread)
}

// TestRoundTripPopulated is the same assertion with values in every shape the
// core declarations use: enums, states, secrets, digests, instants, references,
// repeated scalars, repeated objects, a nested object, and json.
func TestRoundTripPopulated(t *testing.T) {
	roundTrip(t, "actor", &corekinds.Actor{
		Version:   i64(1),
		Authority: str("core.substrate.reamde.dev"),
		Source:    ptr(corekinds.ActorSourceBuiltin),
		Tier:      ptr(corekinds.ActorTierOwner),
	}, corekinds.DecodeActor)

	roundTrip(t, "agent", &corekinds.Agent{
		Version:   i64(8),
		Authority: str("core.substrate.reamde.dev"),
		Prompt:    str("be useful"),
		Provider:  refPath("core.substrate.reamde.dev/llmprovider/default"),
		Model:     str("gpt-5"),
		Params:    &corekinds.AgentParams{Temperature: f64(0.2), MaxTokens: i64(2048)},
		// One arm, two entries: a host built-in named by identity, and a bundle's
		// function carrying an alias.
		Tools: []corekinds.AgentTools{
			{Function: refPath("core.substrate.reamde.dev/function/core.substrate.reamde.dev/query")},
			{Function: refPath("core.substrate.reamde.dev/function/web.bundles.substrate.reamde.dev/setclass"), Name: str("classify")},
		},
		// Absent and empty are different answers: `agents` names one sub-agent,
		// `permissions.writes` names none.
		Agents:  []corekinds.ReferencePath{"core.substrate.reamde.dev/agent/core.substrate.reamde.dev/titler"},
		Budgets: &corekinds.AgentBudgets{MaxTurns: i64(8), Depth: i64(3)},
		Permissions: &corekinds.AgentPermissions{
			Writes: []corekinds.ReferencePath{},
			Reads:  &corekinds.AgentPermissionsReads{Kinds: []corekinds.ReferencePath{"core.substrate.reamde.dev/kind/tasks.substrate.reamde.dev/task"}},
		},
		SubagentOnly: boolean(true),
	}, corekinds.DecodeAgent)

	roundTrip(t, "authority", &corekinds.Authority{
		Version:          i64(2),
		Actors:           []string{"function:tasks"},
		Source:           ptr(corekinds.AuthoritySourceInstalled),
		Quarantined:      boolean(false),
		QuarantineReason: str(""),
	}, corekinds.DecodeAuthority)

	roundTrip(t, "blob", &corekinds.Blob{
		Digest:    str(corekinds.BlobDigestPrefix + testDigest),
		Size:      i64(4096),
		Name:      str("invoice.pdf"),
		MimeType:  str("application/pdf"),
		CreatedBy: str("api"),
		Status:    ptr(corekinds.BlobStatusStored),
	}, corekinds.DecodeBlob)

	roundTrip(t, "bundle", &corekinds.Bundle{
		Version:   i64(1),
		Authority: str("tasks.bundles.substrate.reamde.dev"),
		Inputs: map[string]corekinds.BundleInputs{
			"connector": {
				Kind:   refPath("core.substrate.reamde.dev/kind/tasks.bundles.substrate.reamde.dev/config"),
				Inject: ptr(corekinds.BundleInputsInjectFunctions),
			},
		},
		Installs:    []string{"tasks.bundles.substrate.reamde.dev/config"},
		Requires:    []string{"tasks.substrate.reamde.dev"},
		Modules:     map[string]string{"shared.py": "def helper():\n    return 1\n"},
		Oauth2:      &corekinds.BundleOauth2{ClientInput: str("client"), TokenEndpoint: str("https://example.com/token")},
		Disabled:    boolean(false),
		Uninstalled: boolean(false),
		Purging:     boolean(false),
	}, corekinds.DecodeBundle)

	roundTrip(t, "credential", &corekinds.Credential{
		Username:    str("ada"),
		PasswordRef: secret("sealed:1:pw"),
		// The redaction sentinel is what a READ hands back, and handing it
		// straight on must not be a refusal.
		TotpRef: secret(corekinds.Redacted),
	}, corekinds.DecodeCredential)

	roundTrip(t, "function", &corekinds.Function{
		Version:   i64(1),
		Authority: str("firecrawl.substrate.reamde.dev"),
		Runtime:   ptr(corekinds.FunctionRuntimePython),
		Source:    str("def main(input, host):\n    return {}\n"),
		TimeoutMs: i64(5000),
		Arguments: []corekinds.FunctionArguments{
			{Name: str("query"), Type: ptr(corekinds.FunctionArgumentsTypeString), Required: boolean(true)},
			{Name: str("mode"), Type: ptr(corekinds.FunctionArgumentsTypeEnum), Values: []string{"fast", "thorough"}},
		},
		Returns: []corekinds.FunctionReturns{{Name: str("results"), Type: ptr(corekinds.FunctionReturnsTypeJson)}},
		Permissions: &corekinds.FunctionPermissions{
			Writes:    []corekinds.ReferencePath{"core.substrate.reamde.dev/kind/firecrawl.substrate.reamde.dev/webdocument"},
			Network:   []string{"api.example.com"},
			Mutations: []corekinds.FunctionPermissionsMutations{corekinds.FunctionPermissionsMutationsMerge},
		},
	}, corekinds.DecodeFunction)

	roundTrip(t, "kind", &corekinds.Kind{
		Authority: str("tasks.substrate.reamde.dev"),
		Version:   i64(1),
		Source:    ptr(corekinds.KindSourceInstalled),
		Names:     &corekinds.KindNames{Singular: str("task"), Plural: str("tasks")},
		// The declaration OF a declaration: every container the property dialect
		// has, at the depth the meta-kind describes.
		DisplayTemplate: str("{title}"),
		Traits:          []string{"temporal(point: dueAt)"},
		Indices:         []corekinds.KindIndices{{Properties: []string{"status", "dueAt"}}},
		Edges: map[string]corekinds.KindEdges{
			"project": {To: str("tasks.substrate.reamde.dev/project"), Many: boolean(false), Inverse: str("tasks")},
		},
		// The property declarations are the meta-kind's one json leaf (kind.yaml
		// says why), so they travel as the loader admits them.
		Properties: map[string]any{
			"note":   map[string]any{"type": "text", "fts": false},
			"status": map[string]any{"type": "state", "states": []any{"open", "done"}, "initial": "open"},
			"origin": map[string]any{"type": "object", "fields": map[string]any{"url": map[string]any{"type": "url"}}},
		},
	}, corekinds.DecodeKind)

	roundTrip(t, "llmmessage", &corekinds.LLMMessage{
		Role:       str("assistant"),
		Content:    str("done"),
		Turn:       i64(3),
		ToolCalls:  []corekinds.LLMMessageToolCalls{{Id: str("c1"), Name: str("query"), Arguments: str("{}")}},
		ToolCallId: str("c1"),
		Tool:       str("query"),
		Ok:         boolean(true),
		Thread:     refPath("core.substrate.reamde.dev/llmthread/t1"),
	}, corekinds.DecodeLLMMessage)

	roundTrip(t, "llmprovider", &corekinds.LLMProvider{
		Name:    str("the host gateway"),
		Wire:    ptr(corekinds.LLMProviderWireOpenai),
		BaseURL: str("https://example.com/v1"),
		ApiKey:  secret("sealed:1:key"),
		Headers: []corekinds.LLMProviderHeaders{
			{Name: str("x-attribution"), Value: str("substrate")},
			// A field left absent inside a repeated object stays absent.
			{Name: str("x-tenant")},
		},
		Defaults: &corekinds.LLMProviderDefaults{Temperature: f64(0.2), MaxTokens: i64(2048)},
		Pricing: []corekinds.LLMProviderPricing{
			{Model: str("gpt-5"), InputPer1M: str("1.25"), OutputPer1M: str("10")},
		},
	}, corekinds.DecodeLLMProvider)

	roundTrip(t, "llmthread", &corekinds.LLMThread{
		Agent:            refPath("core.substrate.reamde.dev/agent/core.substrate.reamde.dev/assistant"),
		Provider:         str("default"),
		Model:            str("gpt-5"),
		Mode:             str("chat"),
		Status:           str("succeeded"),
		AgentDepth:       i64(0),
		Turns:            i64(4),
		ToolCalls:        i64(2),
		PromptTokens:     i64(1200),
		CompletionTokens: i64(300),
		TotalTokens:      i64(1500),
		CostUSD:          f64(0.0042),
		StartedAt:        str(testInstant),
		FinishedAt:       str("2026-08-14T09:42:00Z"),
		Parent:           refPath("core.substrate.reamde.dev/llmthread/t0"),
	}, corekinds.DecodeLLMThread)

	roundTrip(t, "propertytype", &corekinds.PropertyType{
		Version:   i64(1),
		Authority: str("shopping.substrate.reamde.dev"),
		Base:      ptr(corekinds.PropertyTypeBaseString),
		Pattern:   str("^[A-Z0-9]{10}$"),
		Values:    []corekinds.PropertyTypeValues{{Value: str("chore"), Label: str("Chore")}},
	}, corekinds.DecodePropertyType)

	roundTrip(t, "recordmapping", &corekinds.RecordMapping{
		Version:   i64(1),
		Authority: str("mail.substrate.reamde.dev"),
		From:      refPath("core.substrate.reamde.dev/kind/mail.substrate.reamde.dev/message"),
		To:        refPath("core.substrate.reamde.dev/kind/mail.substrate.reamde.dev/thread"),
		Edge:      str("thread"),
		Match:     []corekinds.RecordMappingMatch{{From: str("subject"), To: str("title")}},
		Map: map[string]corekinds.RecordMappingMap{
			"title":  {Path: str("subject")},
			"emails": {Path: str("from.address"), Merge: ptr(corekinds.RecordMappingMapMergeUnion)},
		},
	}, corekinds.DecodeRecordMapping)

	roundTrip(t, "recordmerge", &corekinds.RecordMerge{
		Moved: map[string]any{"tasks/a": []any{"tasks/b"}},
	}, corekinds.DecodeRecordMerge)

	roundTrip(t, "recordmergerequest", &corekinds.RecordMergeRequest{
		Rationale: str("same person, two rows"),
		Evidence:  map[string]any{"email": "ada@example.com"},
		Decision:  ptr(corekinds.RecordMergeRequestDecisionAccepted),
		DecidedAt: str(testInstant),
	}, corekinds.DecodeRecordMergeRequest)

	roundTrip(t, "recordpatchrequest", &corekinds.RecordPatchRequest{
		Op:            ptr(corekinds.RecordPatchRequestOpPatch),
		TargetKind:    str("tasks.substrate.reamde.dev/task"),
		TargetId:      str("t-1"),
		TargetVersion: i64(7),
		Diff:          map[string]any{"properties": map[string]any{"title": "renamed"}},
		Rationale:     str("the title was wrong"),
		Decision:      ptr(corekinds.RecordPatchRequestDecisionProposed),
	}, corekinds.DecodeRecordPatchRequest)

	roundTrip(t, "recordsplit", &corekinds.RecordSplit{
		Result: []any{vocabulary.RecordPath("tasks.substrate.reamde.dev/task", "t-2")},
	}, corekinds.DecodeRecordSplit)

	roundTrip(t, "recoverykey", &corekinds.RecoveryKey{
		Algorithm: str("age-x25519"),
		PublicKey: str("age1exampleexampleexample"),
		SealedKey: str("sealed"),
	}, corekinds.DecodeRecoveryKey)

	roundTrip(t, "repository", &corekinds.Repository{
		Name:      str("ada"),
		Lifecycle: ptr(corekinds.RepositoryLifecycleActive),
	}, corekinds.DecodeRepository)

	roundTrip(t, "run", &corekinds.Run{
		Trigger:    str("core.substrate.reamde.dev/nightly"),
		Callable:   str("function:sync"),
		Mode:       str("schedule"),
		Seq:        i64(12),
		FireId:     str("f-1"),
		Record:     str("tasks.substrate.reamde.dev/task/t-1"),
		Status:     str("succeeded"),
		Attempt:    i64(1),
		StartedAt:  str(testInstant),
		FinishedAt: str("2026-08-14T09:41:03Z"),
		Effects:    map[string]int64{"put": 2, "patch": 1},
		Pages:      i64(1),
	}, corekinds.DecodeRun)

	roundTrip(t, "token", &corekinds.Token{
		Label:     str("laptop"),
		Hash:      digest(testDigest),
		ExpiresAt: str(testInstant),
	}, corekinds.DecodeToken)

	roundTrip(t, "trait", &corekinds.Trait{
		Version:    i64(1),
		Authority:  str("core.substrate.reamde.dev"),
		Properties: map[string]string{"tokenRef": "secret"},
		OneOf: []corekinds.TraitOneOf{
			{Name: str("point"), Properties: map[string]string{"at": "datetime"}},
			{Name: str("range"), Properties: map[string]string{"at": "datetime", "endsAt": "datetime"}},
		},
	}, corekinds.DecodeTrait)

	roundTrip(t, "trigger", &corekinds.Trigger{
		Enabled: boolean(true),
		Source: &corekinds.TriggerSource{
			Kind:     ptr(corekinds.TriggerSourceKindSchedule),
			Schedule: &corekinds.TriggerSourceSchedule{Recurrence: str("RRULE:FREQ=DAILY"), Timezone: str("UTC")},
		},
		Callable: refPath("core.substrate.reamde.dev/function/core.substrate.reamde.dev/sync"),
	}, corekinds.DecodeTrigger)
}

// TestNullIsAbsence pins the dialect's delete marker: a null in the map decodes
// as absence, so a round trip through a read that carried one does not
// materialize a value.
func TestNullIsAbsence(t *testing.T) {
	got, problems := corekinds.DecodeBlob(map[string]any{"name": nil, "size": nil})
	if len(problems) > 0 {
		t.Fatalf("a null is a delete marker, not a problem: %v", problems)
	}
	if got.Name != nil || got.Size != nil {
		t.Errorf("a null decoded as a value: %+v", got)
	}
}

// TestDecodeRefuses is the other half of Decode: what it will not admit. Every
// case here is a row the generated types must keep out of a typed field.
func TestDecodeRefuses(t *testing.T) {
	cases := map[string]map[string]any{
		"an undeclared key":       {"nope": "x"},
		"a number where a string": {"name": 4},
		"a string where an int":   {"size": "4096"},
		"a fractional int":        {"size": 4.5},
		"a state outside its set": {"status": "elsewhere"},
	}
	for name, props := range cases {
		t.Run(name, func(t *testing.T) {
			if got, problems := corekinds.DecodeBlob(props); len(problems) == 0 {
				t.Errorf("admitted %v as %+v", props, got)
			}
		})
	}
	// A declared bound broken, which the blob declaration has no room for.
	if _, problems := corekinds.DecodeLLMProvider(map[string]any{
		"defaults": map[string]any{"temperature": 9.0},
	}); len(problems) == 0 {
		t.Error("a temperature outside the declared range was admitted")
	}
}

// TestRequiredIsMetadata pins the boundary a `required:` hint sits on. The write
// path does NOT enforce it (vocabulary Property.Required says so in as many
// words), so a stored row may lack a required property — and these types decode
// stored rows. A Decode that refused one would refuse rows the substrate itself
// admitted, which is why requiredness is data and a method here, and not a
// problem.
func TestRequiredIsMetadata(t *testing.T) {
	if want := []string{"thread"}; !reflect.DeepEqual(corekinds.LLMMessageRequired, want) {
		t.Errorf("LLMMessageRequired is %v, declared %v", corekinds.LLMMessageRequired, want)
	}
	got, problems := corekinds.DecodeLLMMessage(map[string]any{"role": "user"})
	if len(problems) > 0 {
		t.Fatalf("an absent required property is not a decode problem: %v", problems)
	}
	if missing := got.Missing(); !reflect.DeepEqual(missing, []string{"thread"}) {
		t.Errorf("Missing is %v, expected the absent required property", missing)
	}
	answered := &corekinds.LLMMessage{Thread: refPath("core.substrate.reamde.dev/llmthread/t1")}
	if missing := answered.Missing(); len(missing) != 0 {
		t.Errorf("Missing is %v with the requirement answered", missing)
	}
}

// TestDecodeReadsBackWhatStorageWrote holds the decoder to the spellings a value
// arrives in: jsonb reads a number back as float64, and refusing that would
// refuse every row that has been through the database.
func TestDecodeReadsBackWhatStorageWrote(t *testing.T) {
	got, problems := corekinds.DecodeBlob(map[string]any{"size": float64(4096)})
	if len(problems) > 0 {
		t.Fatalf("float64 from jsonb: %v", problems)
	}
	if got.Size == nil || *got.Size != 4096 {
		t.Errorf("size decoded as %v", got.Size)
	}
}

// TestIntegersKeepEveryBit is the adversarial half of conformance 3. An int
// above 2^53 has no exact float64, so a decoder that read every integer through
// one would hand back 9007199254740992 for 9007199254740993 — a stored value
// silently becoming a different stored value, in the one type where nobody
// would look.
func TestIntegersKeepEveryBit(t *testing.T) {
	for _, n := range []int64{1 << 53, 1<<53 + 1, 9007199254740993, 1<<62 + 1, -(1<<53 + 1)} {
		fixture := &corekinds.Blob{Size: &n}
		props := fixture.Encode()
		got, problems := corekinds.DecodeBlob(props)
		if len(problems) > 0 {
			t.Fatalf("%d: %v", n, problems)
		}
		if got.Size == nil || *got.Size != n {
			t.Errorf("%d decoded as %v", n, got.Size)
		}
	}
	// json.Number is the wire's spelling and keeps its bits the same way.
	got, problems := corekinds.DecodeBlob(map[string]any{"size": json.Number("9007199254740993")})
	if len(problems) > 0 {
		t.Fatalf("json.Number: %v", problems)
	}
	if got.Size == nil || *got.Size != 9007199254740993 {
		t.Errorf("json.Number decoded as %v", got.Size)
	}
	// A float64 past the line is REFUSED rather than rounded: which integer it
	// was is no longer in the value, and guessing would store the guess.
	if _, problems := corekinds.DecodeBlob(map[string]any{"size": float64(1 << 53)}); len(problems) == 0 {
		t.Error("an integer beyond exact float precision was admitted from a float")
	}
}

// TestDecodeReadsStoredShapes is the pinned answer to "why does this admit a
// baseURL that is not a URL". The generated decoder is NOT the write-admission
// gate: engine coerceProps refuses a non-absolute URL, an unparseable mailbox,
// a phone that is not E.164 and a time zone the machine does not know, and every
// stored value has already been through it. Repeating those grammars here would
// be a second copy to keep in step, and the only rows it could catch are rows an
// older binary admitted — which refusing now would lock out of their own
// repository.
//
// If that boundary ever moves, this test is what has to be rewritten first.
func TestDecodeReadsStoredShapes(t *testing.T) {
	// A shape the write path would refuse and a stored row cannot hold: admitted,
	// deliberately.
	if _, problems := corekinds.DecodeLLMProvider(map[string]any{"baseURL": "not a URL"}); len(problems) > 0 {
		t.Errorf("the decoder took on the write path's job: %v", problems)
	}
	// The one place it is STRICTER: a datetime must be RFC 3339, because
	// coerceProps normalizes every admitted spelling to RFC 3339 Nano before
	// storing. A stored datetime that is not one did not come from a write.
	if _, problems := corekinds.DecodeRun(map[string]any{"startedAt": "2026-08-14"}); len(problems) == 0 {
		t.Error("a relaxed datetime spelling was admitted as a stored value")
	}
	if _, problems := corekinds.DecodeRun(map[string]any{"startedAt": testInstant}); len(problems) > 0 {
		t.Errorf("the stored datetime form was refused: %v", problems)
	}
}

func ptr[T any](v T) *T { return &v }

func digest(s string) *corekinds.Digest {
	d := corekinds.Digest(s)
	return &d
}
