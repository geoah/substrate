package vocabulary

// WHERE A DECLARATION NAMES SOMETHING (decision record 0259). The repository's
// name is folded into `~` at a reference position and nowhere else, whatever
// a string elsewhere looks like: an agent's `model` is a provider's string,
// a function's `network` an address, a default the author's value. Every key
// of the dialect's closed key sets (testdata/dialect.golden.json) is classed
// here, by the key set the loader reads it with, and
// TestEveryDialectKeyIsClassed fails on a key the table does not class.

// position is what a declaration key's value is.
type position int

const (
	// posWord holds no name: a atWord, a number, a flag, a property's name.
	posWord position = iota + 1
	// posText is the author's atText, never read: a description, a prompt, a
	// function's source, a default, an enum's values, a pattern, a filter.
	posText
	// posRef names something: a kind, a trait, a datatype, a package, a
	// function, an agent or a model, as a string, a record path, a glob or a
	// trait pattern, a reference value's object, or a list of any of them.
	posRef
	// posAuthority is the declaring authority: the name itself is `~`.
	posAuthority
	// posPackages is a map keyed by package references (`requiresAtLeast`).
	posPackages
	// posObject is an object, or a list of objects, of another key set.
	posObject
	// posNamed is a map keyed by names the author chose, each value an
	// object of another key set.
	posNamed
)

// keyClass is one key's position, and for an object or a named map the key
// set its values are read with.
type keyClass struct {
	pos  position
	keys string
}

var (
	atWord      = keyClass{pos: posWord}
	atText      = keyClass{pos: posText}
	atRef       = keyClass{pos: posRef}
	atAuthority = keyClass{pos: posAuthority}
)

func atObject(keys string) keyClass { return keyClass{pos: posObject, keys: keys} }
func atNamed(keys string) keyClass  { return keyClass{pos: posNamed, keys: keys} }

// propertyKeys is the key set a declared property is read with: a scalar's,
// a reference's, an object's and a state machine's keys are one object's,
// classed alike where they share a key.
const propertyKeys = "property"

// propertyKeySets are the dialect's key sets propertyKeys unites.
var propertyKeySets = []string{"propKeys", "referencePropKeys", "objectPropKeys", "machineKeys"}

// declarationPositions classes every key of every key set a declaration's
// data is read with. A key set that is no declaration's data is in
// notDeclarationData instead.
var declarationPositions = map[string]map[string]keyClass{
	"typeDataKeys": {
		"authority": atAuthority, "package": atWord, "description": atText, "displayTemplate": atText,
		"ids": atObject("idsKeys"), "indices": atObject("indexKeys"), "label": atObject("labelKeys"),
		"movedFrom": atRef, "names": atObject("namesKeys"), "properties": atNamed(propertyKeys),
		"purpose": atWord, "retention": atWord, "retired": atObject("kindRetiredKeys"), "traits": atRef,
		"version": atWord,
	},
	"capabilityDataKeys": {
		"authority": atAuthority, "package": atWord, "description": atText,
		"oneOf": atObject("traitVariantKeys"), "properties": atNamed(propertyKeys),
	},
	"traitVariantKeys": {"name": atWord, "properties": atNamed(propertyKeys)},
	"datatypeDataKeys": {
		"authority": atAuthority, "package": atWord, "description": atText, "base": atRef,
		"comparator": atWord, "delta": atWord, "max": atWord, "min": atWord, "pattern": atText,
		"unit": atWord, "units": atWord, "values": atText,
	},
	"packageDataKeys": {
		"authority": atAuthority, "package": atWord, "description": atText, "dialect": atWord,
		"interface": atWord, "retention": atWord, "retired": atObject("packageRetiredKeys"),
		"stability": atWord, "version": atWord,
	},
	"packageRetiredKeys": {"kinds": atWord},
	"bundleDataKeys": {
		"authority": atAuthority, "package": atWord, "description": atText,
		"inputs": atNamed("bundleInputKeys"), "installs": atRef, "modules": atText, "oauth2": atText,
		"requires": atRef, "requiresAtLeast": {pos: posPackages},
	},
	"bundleInputKeys": {"description": atText, "inject": atWord, "kind": atRef},
	"functionDataKeys": {
		"authority": atAuthority, "package": atWord, "description": atText, "approval": atWord,
		"arguments": atObject("functionArgKeys"), "confirmation": atText, "effect": atWord,
		"execution": atWord, "offer": atObject("functionOfferKeys"),
		"permissions": atObject("functionPermissionKeys"), "returns": atObject("functionArgKeys"),
		"runtime": atWord, "source": atText, "stream": atWord, "timeout": atWord,
	},
	"functionArgKeys":   {"description": atText, "name": atWord, "repeated": atWord, "required": atWord, "type": atRef, "values": atText},
	"functionOfferKeys": {"agents": atRef, "name": atWord},
	"functionPermissionKeys": {
		"agents": atRef, "call": atRef, "mutations": atWord, "network": atText,
		"reads": atObject("functionReadsKeys"), "writes": atRef,
	},
	"functionReadsKeys":  {"budgets": atObject("functionBudgetKeys"), "kinds": atRef},
	"functionBudgetKeys": {"calls": atWord, "rows": atWord},
	"agentDataKeys": {
		"authority": atAuthority, "package": atWord, "description": atText,
		"budgets": atObject("agentBudgetKeys"), "compaction": atObject("agentCompactionKeys"),
		"context": atObject("agentContextKeys"), "hiddenFromChat": atWord,
		// A model is the provider's string, never a name of this substrate's.
		"model": atWord, "models": atWord,
		"params": atText, "permissions": atObject("agentPermissionKeys"), "prompt": atText,
		"provider": atRef, "providerInput": atText, "purpose": atWord, "resume": atWord,
		"runsOn": atRef, "subagents": atRef, "tools": atObject("agentToolKeys"),
		"toolsFrom": atObject("agentToolsFromKeys"),
	},
	"agentBudgetKeys": {
		"deadlineSeconds": atWord, "depth": atWord, "maxParallelTools": atWord, "maxToolCalls": atWord,
		"maxToolResultBytes": atWord, "maxTurns": atWord, "spendCentsPerDay": atWord, "toolTimeoutSeconds": atWord,
	},
	"agentCompactionKeys": {"enabled": atWord, "keepRecentTokens": atWord, "replayTools": atWord, "reserveTokens": atWord},
	"agentContextKeys":    {"maxBytes": atWord, "pinned": atObject("agentPinKeys")},
	"agentPinKeys":        {"fields": atWord, "filter": atText, "first": atWord, "kind": atRef, "orderBy": atWord},
	"agentParamKeys": {
		"engineGuidance": atText, "maxTokens": atWord, "reasoningEffort": atWord,
		"reasoningSummary": atWord, "temperature": atWord,
	},
	"agentPermissionKeys": {"reads": atObject("functionReadsKeys"), "writes": atRef},
	"agentToolKeys":       {"background": atWord, "description": atText, "function": atRef, "name": atWord},
	"agentToolsFromKeys":  {"functions": atRef},
	"mappingDataKeys": {
		"authority": atAuthority, "package": atWord, "description": atText, "from": atRef,
		"map": atNamed("mapRuleKeys"), "match": atObject("matchRuleKeys"), "onAmbiguous": atWord,
		"property": atWord, "to": atRef, "where": atText,
	},
	"mapRuleKeys":       {"merge": atWord, "path": atWord},
	"matchRuleKeys":     {"fold": atWord, "from": atWord, "to": atWord},
	"authorityDataKeys": {"description": atText, "version": atWord},
	"idsKeys":           {"type": atWord},
	"indexKeys":         {"properties": atWord},
	"labelKeys":         {"plural": atText, "singular": atText},
	"namesKeys":         {"singular": atText},
	"kindRetiredKeys":   {"properties": atWord, "states": atWord, "values": atWord},
	"propKeys": {
		"base": atRef, "comparator": atWord, "default": atText, "delta": atWord, "deprecated": atWord,
		"description": atText, "displayName": atText, "done": atWord, "dropped": atWord, "embed": atWord,
		"fields": atNamed(propertyKeys), "fts": atWord, "keyPattern": atText, "keyed": atWord,
		"managed": atWord, "max": atWord, "mediaTypes": atWord, "min": atWord, "pattern": atText,
		"proposed": atWord, "renamedFrom": atWord, "repeated": atWord, "required": atWord, "type": atRef,
		"unique": atWord, "unit": atWord, "units": atWord, "values": atText, "writer": atWord,
	},
	"referencePropKeys": {
		"deprecated": atWord, "description": atText, "displayName": atText, "inverse": atWord,
		"inverseDescription": atText, "keyPattern": atText, "keyed": atWord, "kind": atRef,
		"managed": atWord, "mustExist": atWord, "onDelete": atWord, "properties": atNamed("linkPropKeys"),
		"renamedFrom": atWord, "repeated": atWord, "required": atWord, "subject": atWord, "trait": atRef,
		"type": atRef, "unique": atWord, "writer": atWord,
	},
	"objectPropKeys": {
		"deprecated": atWord, "description": atText, "displayName": atText, "fields": atNamed(propertyKeys),
		"keyPattern": atText, "keyed": atWord, "managed": atWord, "repeated": atWord, "required": atWord,
		"type": atRef, "writer": atWord,
	},
	"machineKeys": {
		"deprecated": atWord, "description": atText, "displayName": atText, "done": atWord, "dropped": atWord,
		"initial": atWord, "proposed": atWord, "states": atWord, "transitions": atObject("transitionKeys"),
		"type": atRef,
	},
	"transitionKeys": {"from": atWord, "notifies": atWord, "onEnter": atWord, "stamps": atWord, "to": atWord},
	"linkPropKeys": {
		"comparator": atWord, "delta": atWord, "deprecated": atWord, "description": atText,
		"displayName": atText, "max": atWord, "min": atWord, "pattern": atText, "required": atWord,
		"type": atRef, "unit": atWord, "units": atWord, "values": atText,
	},
	"enumValueKeys":     {"deprecated": atWord, "label": atText, "renamedFrom": atWord, "value": atText},
	"quantityBoundKeys": {"unit": atWord, "value": atWord},
	"oauth2MetaKeys": {
		"authorizationEndpoint": atText, "clientInput": atText, "emailEndpoint": atText,
		"emailProperty": atText, "featureScopes": atText, "revocationEndpoint": atText, "tokenEndpoint": atText,
	},
	"featureScopeKeys": {"access": atText, "scopes": atText},
}

// notDeclarationData are the dialect's key sets that are no declaration's
// data: the envelope a document travels in, and a shipped record's, whose
// ids the document walk reads (localizeOwnDocument).
var notDeclarationData = map[string]bool{
	"envelopeKeys": true, "metadataKeys": true, "shippedRecordKeys": true,
	"shippedRecordDataKeys": true, "shippedRecordMetadataKeys": true,
}

// declarationRoots is the key set each declaration kind's data is read with.
var declarationRoots = map[string]string{
	CoreKind(DocKind): "typeDataKeys", CoreKind(DocTrait): "capabilityDataKeys",
	CoreKind(DocPropertyType): "datatypeDataKeys", CoreKind(DocPackage): "packageDataKeys",
	CoreKind(DocBundle): "bundleDataKeys", CoreKind(DocFunction): "functionDataKeys",
	CoreKind(DocAgent): "agentDataKeys", CoreKind(DocRecordMapping): "mappingDataKeys",
	CoreKind(DocAuthority): "authorityDataKeys",
}

// classOf is key's position in an object of the key set keys.
func classOf(keys, key string) (keyClass, bool) {
	if keys == propertyKeys {
		for _, set := range propertyKeySets {
			if c, ok := declarationPositions[set][key]; ok {
				return c, true
			}
		}
		return keyClass{}, false
	}
	c, ok := declarationPositions[keys][key]
	return c, ok
}
