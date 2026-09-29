package main

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/geoah/substrate/internal/vocabulary"
)

// The naming half of kinds:check. The loader reads one declaration at a time
// and can only refuse a name that is not one lowercase word; these two rules
// read the vocabulary around a kind, so they run here, over every kind id the
// diff ADDS (a rename is one: its new id is added):
//
//   - its package and name segments carry no dead word from docs/terms.md;
//   - a name that ends like a member of a family in its package starts with
//     the family's stem, or with another kind of the package.
//
// Whether a description is plain, or whether a link between records is a
// reference property rather than a kind of its own, is judgment, and the
// kind-review skill under .claude/skills/ reads for it. Nothing here is
// advisory: every rule refuses the merge.

// deadWord is one word docs/terms.md retired, with what took its job.
type deadWord struct {
	word        string
	replacement string
}

// terms is what the gate reads off docs/terms.md.
type terms struct {
	dead []deadWord
	// live is every replacement that is one word the page's tables define
	// (record, kind, reference, ...): the words the dead ones stood beside
	// when they were in use.
	live []string
}

var (
	// deadClause is one `**word** → replacement` pair of terms.md's
	// "Dead words" paragraph, or a run of words sharing one replacement
	// (`**relationship** and **edge** → reference`). Only the bold words
	// BEFORE an arrow are read as dead, so the live word after it (record,
	// kind, authority) is never taken for one, bolded or not.
	deadClause = regexp.MustCompile(`((?:\*\*[^*]+\*\*\s+and\s+)*\*\*[^*]+\*\*)\s*→\s*([^,.;]+)`)
	boldSpan   = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	// tableTerm is the term one row of the page's tables defines.
	tableTerm = regexp.MustCompile(`(?m)^\| \*\*([^*]+)\*\* \|`)
)

// parseTerms reads the dead words out of docs/terms.md, which lists them in
// one paragraph opening "Dead words". A page without that paragraph, or one
// naming no word, is an error: a gate that parsed nothing would pass having
// checked nothing.
func parseTerms(md string) (terms, error) {
	var para string
	for p := range strings.SplitSeq(md, "\n\n") {
		if strings.HasPrefix(strings.TrimSpace(p), "Dead words") {
			para = strings.Join(strings.Fields(p), " ")
			break
		}
	}
	if para == "" {
		return terms{}, errors.New(`no paragraph opens with "Dead words"`)
	}
	defined := map[string]bool{}
	for _, m := range tableTerm.FindAllStringSubmatch(md, -1) {
		defined[m[1]] = true
	}
	var out terms
	for _, m := range deadClause.FindAllStringSubmatch(para, -1) {
		replacement := strings.TrimSpace(m[2])
		for _, b := range boldSpan.FindAllStringSubmatch(m[1], -1) {
			word := strings.ToLower(strings.Join(strings.Fields(b[1]), ""))
			out.dead = append(out.dead, deadWord{word: word, replacement: replacement})
		}
		if defined[replacement] {
			out.live = union(out.live, []string{replacement})
		}
	}
	if len(out.dead) == 0 {
		return terms{}, errors.New(`the "Dead words" paragraph names no **word** → replacement pair`)
	}
	return out, nil
}

// honestCompounds is the dead words an honest name still carries inside a
// compound, each with the name or the sense that shows it. The gate refuses
// them as a whole segment (a kind called `type` or `logs`) and glued to a
// live word (`recordtype`, `kindgroup`, `incomingreference`), which is the
// dead sense spelled out; any other compound is the review skill's to read.
// Every other dead word is refused anywhere in a segment: each is long and
// specific enough that no honest name carries it by accident (`identity`
// carries `entity`, and is dead itself). An English word that merely holds
// one (`lieutenant`, `disintegration`) is refused as well, and takes an entry
// here when a kind needs it. A word that joins terms.md and collides with a
// shipped name fails TestShippedTreesPassTheNameGate, and the fix is an entry
// here with its reason, never an exemption for the name.
var honestCompounds = map[string]string{
	"type":      "`propertytype` is a live term and GitHub's `issuetype` is the upstream's own noun",
	"group":     "Google's `contactgroup` is the upstream's own noun",
	"log":       "`tasklog` is a sample's work log, and `catalog` and `backlog` only end in the letters",
	"schema":    "a function's JSON Schema is a live sense, and `schematic` only starts with the letters",
	"extension": "a file extension and a Postgres extension are live senses",
	"edge":      "`knowledge` and `pledge` only end in the letters",
	"incoming":  "an upstream's incoming message or webhook is an honest noun; the dead sense is the removed `/incoming` route",
	"plural":    "`plurality` only starts with the letters; the dead sense is the retired `names.plural` key",
}

// forms is the spellings a segment may carry the word in: itself and its
// plural.
func (d deadWord) forms() []string {
	out := []string{d.word, d.word + "s"}
	if stem, ok := strings.CutSuffix(d.word, "y"); ok {
		out = append(out, stem+"ies")
	}
	return out
}

// in reports whether one segment of a kind id carries the word.
func (d deadWord) in(segment string, live []string) bool {
	_, honest := honestCompounds[d.word]
	for _, f := range d.forms() {
		if segment == f || (!honest && strings.Contains(segment, f)) {
			return true
		}
		if !honest {
			continue
		}
		for _, l := range live {
			if segment == l+f || segment == f+l {
				return true
			}
		}
	}
	return false
}

// minStem is the shortest prefix that makes a family: `record` is one, and
// the four letters `chat` and `task` share with their compounds are not.
const minStem = 6

// family is two or more kinds of one package spelled as one stem the package
// does not declare as a kind, plus a word each: `recordpatch` is the stem of
// recordpatchpolicy and recordpatchrequest.
//
// The stem must not be a kind itself, because a kind stem names an OWNER:
// Google's calendarsync is the sync of a calendar, so a gmailsync beside it
// is the sync of something else, not a misspelling of that family. A stem no
// kind declares (recordpatch) is a concept only the family spells, so the
// words its members end in (policy, request) are the family's.
//
// Both halves of the split must be words the package spells, so the letters
// three kinds happen to share (`recordm` in recordmapping, recordmerge and
// recordmergerequest) make no family.
type family struct {
	stem    string
	members []string
}

// kindFamilies finds every family among one package's kind names.
func kindFamilies(names []string, words map[string]bool) []family {
	isKind := map[string]bool{}
	for _, n := range names {
		isKind[n] = true
	}
	seen := map[string]bool{}
	var out []family
	for _, n := range names {
		for end := minStem; end < len(n); end++ {
			stem := n[:end]
			if seen[stem] {
				continue
			}
			seen[stem] = true
			if isKind[stem] || !spells(stem, words) {
				continue
			}
			var members []string
			for _, m := range names {
				if len(m) > end && strings.HasPrefix(m, stem) && spells(m[end:], words) {
					members = append(members, m)
				}
			}
			if len(members) >= 2 {
				sort.Strings(members)
				out = append(out, family{stem: stem, members: members})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].stem < out[j].stem })
	return out
}

// spells reports whether s splits wholly into words of the set.
func spells(s string, words map[string]bool) bool {
	ok := make([]bool, len(s)+1)
	ok[0] = true
	for i := 1; i <= len(s); i++ {
		for j := 0; j < i && !ok[i]; j++ {
			ok[i] = ok[j] && words[s[j:i]]
		}
	}
	return ok[len(s)]
}

// offFamily is one name ending in a word a family's member ends in, without
// the family's stem.
type offFamily struct {
	fam    family
	member string
	suffix string
}

// findOffFamily finds the family a name ends like and does not start like,
// preferring the longest shared ending. A name starting with another kind of
// the package is spelled after its own owner (recordmergerequest is
// recordmerge plus request, beside recordpatchrequest), so it is not off any
// family. A shorter family stem is no owner: recordwritepolicy keeps
// `record` and still drops the `patch` its family spells.
//
// A common ending is refused too: the rule cannot tell `accessrequest`, a new
// idea, from `writerequest`, a misspelling of recordpatchrequest. The costs
// are uneven. A refusal costs a rename before the merge, and the owner prefix
// is always open (`repositoryaccessrequest`); a missed misspelling ships, and
// renaming a shipped kind takes a `movedFrom` upgrade (decision 0078).
func findOffFamily(name string, fams []family, kinds []string) (offFamily, bool) {
	var best offFamily
	found := false
	for _, f := range fams {
		if strings.HasPrefix(name, f.stem) {
			continue
		}
		for _, m := range f.members {
			suffix := m[len(f.stem):]
			if !strings.HasSuffix(name, suffix) || len(suffix) <= len(best.suffix) {
				continue
			}
			if ownedBy(strings.TrimSuffix(name, suffix), kinds) {
				continue
			}
			best, found = offFamily{fam: f, member: m, suffix: suffix}, true
		}
	}
	return best, found
}

// ownedBy reports whether a name's head starts with a kind of the package.
func ownedBy(head string, kinds []string) bool {
	for _, k := range kinds {
		if strings.HasPrefix(head, k) {
			return true
		}
	}
	return false
}

// nameViolations holds every kind the diff adds to the two naming rules.
func nameViolations(base, head *tree, vocab terms) []string {
	var out []string
	for _, k := range sortedKeys(head.decls) {
		h := head.decls[k]
		if h.kind != vocabulary.DocKind {
			continue
		}
		if _, existed := base.decls[k]; existed {
			continue
		}
		_, pkgName, name := vocabulary.SplitKindRef(h.id)
		for _, d := range vocab.dead {
			for _, seg := range []struct{ what, value string }{{"package", pkgName}, {"name", name}} {
				if d.in(seg.value, vocab.live) {
					out = append(out, fmt.Sprintf("%s: kind %s carries the dead word %q in its %s; docs/terms.md: %s → %s",
						h.file, h.id, d.word, seg.what, d.word, d.replacement))
				}
			}
		}
		if v, off := familyViolation(base, head, h.pkg, name); off {
			out = append(out, fmt.Sprintf("%s: kind %s ends in %q like %s/%s but starts with neither that family's stem %q nor another kind of the package; spell a member of the family with its stem (%s), or start the name with the kind it belongs to",
				h.file, h.id, v.suffix, h.pkg, v.member, v.fam.stem, strings.Join(v.fam.members, ", ")))
		}
	}
	return out
}

// familyViolation checks one added name against the families of its package,
// read without the name itself: its own declaration neither joins a family
// nor adds a word. The families are read off every kind the package has on
// either side of the diff, so a rename is held to the family it leaves; a
// family none of whose members is left in head was renamed away whole, and
// holds nothing. That respelling is one deliberate change review reads as
// such; held to the old stem, no change could ever respell a family.
func familyViolation(base, head *tree, pkg, name string) (offFamily, bool) {
	inHead := map[string]bool{}
	for _, n := range kindNames(head, pkg) {
		inHead[n] = true
	}
	var others []string
	for _, n := range union(kindNames(base, pkg), kindNames(head, pkg)) {
		if n != name {
			others = append(others, n)
		}
	}
	sort.Strings(others)
	words := map[string]bool{}
	for _, n := range others {
		words[n] = true
	}
	for _, d := range head.decls {
		if d.pkg == pkg && (d.kind != vocabulary.DocKind || vocabulary.KindName(d.id) != name) {
			collectWords(d.data, words)
		}
	}
	var fams []family
	for _, f := range kindFamilies(others, words) {
		for _, m := range f.members {
			if inHead[m] {
				fams = append(fams, f)
				break
			}
		}
	}
	return findOffFamily(name, fams, others)
}

// kindNames lists the local names of one package's kinds in a tree.
func kindNames(t *tree, pkg string) []string {
	var out []string
	for _, d := range t.decls {
		if d.kind == vocabulary.DocKind && d.pkg == pkg {
			out = append(out, vocabulary.KindName(d.id))
		}
	}
	sort.Strings(out)
	return out
}

// collectWords adds every word of a declaration's data, keys and string
// values alike, to the set.
func collectWords(v any, into map[string]bool) {
	switch x := v.(type) {
	case string:
		addWords(x, into)
	case map[string]any:
		for k, e := range x {
			addWords(k, into)
			collectWords(e, into)
		}
	case []any:
		for _, e := range x {
			collectWords(e, into)
		}
	}
}

// addWords splits s at every character that is not an ASCII letter and at
// every lower-to-upper step, so `expandReferents` is expand and referents. A
// word under three letters is dropped: with `a`, `of` and `to` in the set,
// almost any run of letters would split into words.
func addWords(s string, into map[string]bool) {
	var word []byte
	flush := func() {
		if len(word) >= 3 {
			into[strings.ToLower(string(word))] = true
		}
		word = word[:0]
	}
	for i := range len(s) {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z':
			if len(word) > 0 && word[len(word)-1] >= 'a' {
				flush()
			}
			word = append(word, c)
		case c >= 'a' && c <= 'z':
			word = append(word, c)
		default:
			flush()
		}
	}
	flush()
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
