package vocabulary

import (
	"fmt"
	"reflect"
)

// THE VOCABULARY DIALECT is the number of the declaration format this loader
// reads: the closed key sets of every declaration document and their meaning
// (decision record 0268, which amends 0020). A key added to any of them is a
// new dialect, so the number goes up with it, and
// testdata/dialect.golden.json records the key sets under the number they
// were admitted at: TestDialectKeysMatchTheGolden refuses a key set that
// moved under an unchanged number. The server publishes the number in
// discovery, a repository is stamped with it at open (engine/dialect.go),
// and a package states the dialect it needs in its header's `dialect:`, and
// each version in its publisher's document, so a server picks the newest
// version it can read instead of failing on the newest one listed.
//
// 1 and 2 are the dialects before the number was held to the keys; 3 adds
// a package header's `stability`, `interface` and `dialect`; 4 takes the actor
// document out, because an actor is a record the substrate writes for
// everything that acts (decision 0270).
const Dialect = 4

// keyIntroducedAt is the dialect each declaration key arrived in, by the
// closed key set that holds it, for every key after dialect 2; a key it does
// not list is read by every dialect. A package's declarations need the
// highest dialect among the keys they use, and a header may not state less
// (checkStatedDialect), so a server older than a key is never told a
// package is written for it. A step that adds a key adds its line here, at
// the dialect it raises Dialect to; the dialect golden holds the table, and
// its test and dialect:check refuse a key added without its line, a line
// whose number moved, and a line dropped while its key stays.
var keyIntroducedAt = map[string]map[string]int64{
	"packageDataKeys": {"stability": 3, "interface": 3, "dialect": 3},
}

// closedKeySets are the closed key sets of the declaration documents, by the
// name the dialect golden records them under, which is the variable's. A set
// is found here by its identity (keySetName), so a key checked against it is
// looked up in keyIntroducedAt without the check naming the set.
var closedKeySets = map[string]map[string]bool{
	"envelopeKeys": envelopeKeys, "metadataKeys": metadataKeys,
	"packageDataKeys": packageDataKeys, "packageRetiredKeys": packageRetiredKeys,
	"authorityDataKeys": authorityDataKeys,
	"datatypeDataKeys":  datatypeDataKeys, "capabilityDataKeys": capabilityDataKeys,
	"traitVariantKeys": traitVariantKeys,
	"typeDataKeys":     typeDataKeys, "kindRetiredKeys": kindRetiredKeys,
	"namesKeys": namesKeys, "indexKeys": indexKeys, "labelKeys": labelKeys, "idsKeys": idsKeys,
	"propKeys": propKeys, "objectPropKeys": objectPropKeys, "referencePropKeys": referencePropKeys,
	"linkPropKeys": linkPropKeys, "enumValueKeys": enumValueKeys,
	"quantityBoundKeys": quantityBoundKeys,
	"machineKeys":       machineKeys, "transitionKeys": transitionKeys,
	"mappingDataKeys": mappingDataKeys, "matchRuleKeys": matchRuleKeys, "mapRuleKeys": mapRuleKeys,
	"functionDataKeys": functionDataKeys, "functionArgKeys": functionArgKeys,
	"functionOfferKeys": functionOfferKeys, "functionPermissionKeys": functionPermissionKeys,
	"functionReadsKeys": functionReadsKeys, "functionBudgetKeys": functionBudgetKeys,
	"agentDataKeys": agentDataKeys, "agentPermissionKeys": agentPermissionKeys,
	"agentBudgetKeys": agentBudgetKeys, "agentCompactionKeys": agentCompactionKeys,
	"agentToolKeys": agentToolKeys, "agentToolsFromKeys": agentToolsFromKeys,
	"agentContextKeys": agentContextKeys, "agentPinKeys": agentPinKeys,
	"agentParamKeys": agentParamKeys,
	"bundleDataKeys": bundleDataKeys, "bundleInputKeys": bundleInputKeys,
	"featureScopeKeys": featureScopeKeys, "oauth2MetaKeys": oauth2MetaKeys,
	"shippedRecordKeys": shippedRecordKeys, "shippedRecordMetadataKeys": shippedRecordMetadataKeys,
	"shippedRecordDataKeys": shippedRecordDataKeys,
}

// keySetNames is closedKeySets turned around: a set's name by its identity.
var keySetNames = func() map[uintptr]string {
	out := make(map[uintptr]string, len(closedKeySets))
	for name, set := range closedKeySets {
		out[reflect.ValueOf(set).Pointer()] = name
	}
	return out
}()

// noteKey records that the package being built uses key k of set: what its
// header may not state a dialect below. A tie keeps the first key by name,
// so a refusal names the same key every time.
func (l *loader) noteKey(set map[string]bool, k string) {
	at := introducedAt(set, k)
	if at > l.needs || (at > 0 && at == l.needs && k < l.needsKey) {
		l.needs, l.needsKey = at, k
	}
}

// noteDocuments counts what each document of the group needs for its
// envelope and its metadata, which are read before the loader
// (documentFrom), toward the package it builds.
func (l *loader) noteDocuments(gd *packageDocs) {
	docs := []*Document{gd.header}
	for _, list := range [][]Document{gd.capabilities, gd.datatypes, gd.types, gd.mappings, gd.functions, gd.agents, gd.bundles} {
		for i := range list {
			docs = append(docs, &list[i])
		}
	}
	for _, d := range docs {
		if d != nil && (d.needs > l.needs || (d.needs > 0 && d.needs == l.needs && d.needsKey < l.needsKey)) {
			l.needs, l.needsKey = d.needs, d.needsKey
		}
	}
}

// introducedAt is the dialect key k of set arrived in, 0 for one every
// dialect reads.
func introducedAt(set map[string]bool, k string) int64 {
	return keyIntroducedAt[keySetNames[reflect.ValueOf(set).Pointer()]][k]
}

// holdRecordsToTheirDialect counts each shipped record's keys toward the need
// of its kind's package, as the loader counts the declarations' keys, and
// refuses a package whose header states less than a record of it needs. A
// record's keys are read where the tree is split (shippedRecordFrom), so
// this is where a record meets its package.
func holdRecordsToTheirDialect(packages []*Package, records []ShippedRecord) error {
	byIdentity := make(map[string]*Package, len(packages))
	for _, g := range packages {
		byIdentity[g.Identity] = g
	}
	var problems []string
	for _, rec := range records {
		g := byIdentity[KindPackage(rec.Kind)]
		if g == nil || rec.needs == 0 {
			continue
		}
		g.NeedsDialect = max(g.NeedsDialect, rec.needs)
		if g.Dialect > 0 && g.Dialect < rec.needs {
			problems = append(problems, fmt.Sprintf(
				"package %s: data.dialect: the package states dialect %d, and its shipped record %s/%s uses `%s`, which dialect %d introduced — state dialect %d, or leave the key out",
				g.Identity, g.Dialect, rec.Kind, rec.ID, rec.needsKey, rec.needs, rec.needs))
		}
	}
	if len(problems) > 0 {
		return validationError(problems)
	}
	return nil
}

// checkStatedDialect holds the dialect a package's header states to what its
// declarations need, and keeps the need on the package. A header that states
// none claims nothing.
func (l *loader) checkStatedDialect(identity string, g *Package) {
	g.NeedsDialect = l.needs
	if g.Dialect > 0 && g.Dialect < l.needs {
		l.errf("package %s: data.dialect: the package states dialect %d, and its declarations use `%s`, which dialect %d introduced — state dialect %d, or leave the key out",
			identity, g.Dialect, l.needsKey, l.needs, l.needs)
	}
}

// parsePackageDialect reads the header's `dialect:`, the declaration format
// the package needs. An absent key stores nothing and needs nothing beyond
// what this loader reads. A number above Dialect is refused naming both, so
// the author learns which server the package needs rather than meeting an
// unknown key further down.
func (l *loader) parsePackageDialect(where string, d map[string]any, g *Package) {
	raw, present := d["dialect"]
	if !present {
		return
	}
	v, ok := VersionValue(raw)
	if !ok || v < 1 {
		l.errf("%s: data.dialect: must be a whole number of at least 1, the vocabulary dialect the package needs, not %v — or drop the key", where, raw)
		return
	}
	if v > Dialect {
		l.errf("%s: data.dialect: %s", where, DialectRefusal(v))
		return
	}
	g.Dialect = v
}

// DialectRefusal is why a package that needs the given dialect is refused
// here, in the words every door uses.
func DialectRefusal(needs int64) string {
	return fmt.Sprintf("the package needs vocabulary dialect %d, and this server reads dialect %d and below — run a newer server, or install a version of the package written for dialect %d",
		needs, Dialect, Dialect)
}
