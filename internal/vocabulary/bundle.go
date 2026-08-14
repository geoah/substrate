package vocabulary

import (
	"fmt"
	"net/url"
	"sort"
	"strings"
)

// A bundle is the install unit (substrate-primitives §4, record 63): one
// document declaring the authority it owns, the INPUTS it configures
// through (each naming a kind), and the exact identities of everything it ships
// into that authority. Install, upgrade and teardown are whole-authority applies —
// the closure the document lists IS the authority, so the loader refuses any
// authority whose declared members and `installs:` disagree. Lifecycle state
// (disable, uninstall) is runtime state on the bundle's record row, never
// part of this declaration.
//
// TWO SHAPES, and only ONE of them is a name rule: a VOCABULARY bundle owns a
// BARE org-domain authority (`people.substrate.reamde.dev`) and ships kinds and
// nothing else; an EXTENSION owns ANY OTHER legal authority and may carry
// inputs, callables and a provider flow. The vocabulary shape is checked
// because it GRANTS something — such an authority is built `source: builtin`
// whichever door it came through (load.go), which keeps its GraphQL names bare
// and its declarations behind the authority chokepoint. It exists because
// repository creation seeds core alone: the substrate's own vocabulary is
// delivered through the registry rather than the seed, and stays SHIPPED
// either way. Nothing is granted by an extension's name, so nothing is checked
// there — `web.bundles.substrate.reamde.dev` is the convention the shipped tree
// keeps, not a rule the loader enforces.
//
// What the fixed `.bundles.` suffix used to guarantee BY CONSTRUCTION is that
// an authority's first label was unique: the label is the bundle's name, its
// `metadata.id` suffix, the actor an install writes under (`bundle:<name>`) and
// the prefix every installed kind's GraphQL name carries (ref.go). With the
// suffix free, two authorities can reach for one label, so the uniqueness is
// checked instead of assumed — Registry.Finalize refuses the second (load.go,
// bundleNameProblems).

// BundleAuthoritySuffix is the CONVENTIONAL category the shipped tree's
// extension authorities carry. It is not admission: an extension authority may
// be spelled any legal way. The loader reads it for one typo-catcher alone — an
// authority wearing the convention with no bundle document beside it.
const BundleAuthoritySuffix = ".bundles.substrate.reamde.dev"

// OrgDomainSuffix is the organization's domain — the namespace a SHIPPED
// authority is bare under ("people.substrate.reamde.dev"). It is never the name of the
// system, only the namespace.
const OrgDomainSuffix = ".substrate.reamde.dev"

// The host-recognized trait interfaces: traits
// the loader and the engine key behavior on, shipped in core.
const (
	// TraitAccountConfig marks a bundle's account types: records are the
	// bundle's connected accounts, enumerated by trait-scoped reads.
	TraitAccountConfig = "accountconfig"
	// TraitOAuth2 marks a kind carrying the standard OAuth client fields;
	// the oauth2 block's clientInput names an input of such a kind.
	TraitOAuth2 = "oauth2"
)

// The core traits' FULL identities — what every host check compares, exactly
// : a bundle can declare its own trait wearing one of the
// bare names above, and its types would satisfy a bare-name check; the
// resolved binding identity is the only thing a local trait cannot spoof.
const (
	TraitAccountConfigCore = AuthorityCore + "/" + TraitAccountConfig
	TraitOAuth2Core        = AuthorityCore + "/" + TraitOAuth2
)

// Bundle is one parsed bundle document.
type Bundle struct {
	// Name is the owned authority's first label ("web" of
	// "web.bundles.substrate.reamde.dev"). It is unique across the registry,
	// which Finalize checks rather than the authority's spelling implying it.
	Name string
	// Authority is the owned authority. The document's id is "<authority>/<name>" like
	// every member kind's — the authority name itself is the authority
	// header's id, and ids are one namespace.
	Authority   string
	Description string
	// Inputs are the bundle's declared configuration needs, by name: each
	// names a KIND whose records satisfy it, and the engine resolves one
	// record per input (bound edge, the id `default`, or the sole live
	// record — in that order). No cardinality is enforced on the kind
	// itself: any number of records may exist, resolution picks one.
	// A bundle with no needs declares no inputs, and nothing anywhere
	// implies configuration.
	Inputs map[string]BundleInput
	// InputOrder is Inputs' name order (sorted): the YAML map loses its
	// authored order in parsing, so sorted names are the one deterministic
	// order status and error output can promise.
	InputOrder []string
	// Installs lists the full identities of everything the bundle ships —
	// its types, traits, property types, mappings, functions and agents.
	// The loader holds it equal to the authority's declared members.
	Installs []string
	// Requires names the AUTHORITIES this bundle's closure declares against —
	// the vocabulary its mappings, edges and trigger subscriptions point at.
	// Vocabulary is imported now rather than seeded, so a bundle that maps
	// google contacts onto `people.substrate.reamde.dev/person` cannot assume people is
	// there. resolveBundle refuses the install when one is absent, naming what
	// to import first, instead of letting the closure fail on an unresolvable
	// edge target.
	Requires []string
	// Vocabulary marks a VOCABULARY bundle: one that owns a BARE authority
	// under the org domain ("people.substrate.reamde.dev") and ships kinds and nothing
	// else — no inputs, no functions, no agents, no OAuth. It is the
	// substrate's own vocabulary, delivered through the registry instead of
	// the creation seed, so its declarations stay SHIPPED (`source: builtin`)
	// and only a substrate path may write them.
	Vocabulary bool
	// Modules are the bundle's SHARED library modules, filename → inline
	// source, that its functions import to dedup helpers (a shared http
	// client, provider auth, normalizers) instead of each ≤256 KiB body
	// re-implementing them. `.py` files go on the Python import path of an
	// isolated per-installation process; `.go` files are vendored into a
	// function's Go build as the `substratefn.local/lib` package. Modules are
	// inline sources on the bundle document, not member records, so they do
	// not participate in the install closure.
	Modules map[string]string

	// OAuth2 is the bundle's TRUSTED OAuth provider metadata, compiled from
	// the manifest — nil when the bundle declares no oauth2 flow. Endpoints
	// and the feature→scope mapping live HERE, on the immutable install
	// artifact, never on the mutable config record: a config-row edit can then
	// never redirect a token exchange, a refresh, or a credential revocation at
	// an attacker's server. The host reads only this.
	OAuth2 *BundleOAuth2

	// Definition is the manifest's data map, exactly as authored.
	Definition map[string]any
	// SourceYAML is the verbatim manifest; installed documents have no
	// original text, so theirs is derived.
	SourceYAML string
}

// BundleInput is one declared configuration need: a kind whose records can
// satisfy it, and who consumes the resolved record.
type BundleInput struct {
	// Kind is the full identity of the kind whose records satisfy this
	// input. Any number of records may exist; the engine resolves one.
	Kind string
	// Inject names the consumer the resolved record is handed to:
	// "functions" injects it (secrets resolved) into every function
	// invocation of the bundle under the input's name; empty means the
	// input is read by a host facility alone (the OAuth client).
	Inject string
	// Description says what the input is for, for the console.
	Description string
}

// BundleInputInjectFunctions is the one Inject value: the resolved record
// crosses into function invocations. Facility-read inputs leave it empty.
const BundleInputInjectFunctions = "functions"

// BundleOAuth2 is a bundle's compiled OAuth provider metadata (review-google
// #1). The client record keeps only the client id and secret; every endpoint
// and the feature→scope mapping are here, admitted from the manifest and
// immutable at runtime.
type BundleOAuth2 struct {
	// ClientInput names the bundle input whose resolved record carries the
	// OAuth CLIENT credentials (an oauth2-trait kind: clientId +
	// clientSecret). Required: an oauth2 block without a client is a flow
	// that can never start.
	ClientInput string
	// AuthorizationEndpoint and TokenEndpoint are required; RevocationEndpoint
	// is optional (best-effort revocation on teardown). All are https absolute
	// URLs (http is admitted only for a loopback host, so tests may run a local
	// provider without TLS — a real provider is always https).
	AuthorizationEndpoint string
	TokenEndpoint         string
	RevocationEndpoint    string
	// FeatureScopes maps an account toggle property name to the OAuth scopes
	// enabling it requests. StartOAuth unions the scopes of the account's TRUE
	// toggles; a toggle absent here (an unwired feature) requests nothing — so
	// gmail/calendar cannot be requested while they remain unmapped.
	FeatureScopes map[string][]string
	// EmailEndpoint is the OPTIONAL provider endpoint (https) the facility GETs
	// with the fresh access token right after the exchange to learn the
	// connected account's own address — Google's People `people/me` or any
	// OIDC userinfo. Absent means the account's email is never auto-derived.
	// Parsed shapes: `{email}` (userinfo) or `{emailAddresses:[{value,
	// metadata:{primary}}]}` (People).
	EmailEndpoint string
	// EmailProperty is the OPTIONAL account property the derived email lands in
	// (writer: oauth, so the facility's actor may set it and the user cannot).
	// Set together with EmailEndpoint; empty disables the write.
	EmailProperty string
}

// Identity is "<authority>/<name>" — the bundle's record id ("web" owning
// web.bundles.substrate.reamde.dev is web.bundles.substrate.reamde.dev/web; the bare authority name is
// the authority header's id).
func (b *Bundle) Identity() string { return KindRef(b.Authority, b.Name) }

var bundleDataKeys = map[string]bool{
	"authority": true, "description": true, "inputs": true, "installs": true,
	"modules": true, "oauth2": true, "requires": true,
}

var oauth2MetaKeys = map[string]bool{
	"authorizationEndpoint": true, "tokenEndpoint": true,
	"revocationEndpoint": true, "featureScopes": true,
	"emailEndpoint": true, "emailProperty": true, "clientInput": true,
}

// ModuleSourceMaxBytes bounds one shared module's inline source — the same
// ≤256 KiB ceiling a function body carries.
const ModuleSourceMaxBytes = SourceMaxBytes

// reservedPyModules are `.py` module base names a bundle may NOT ship: names
// the Python interpreter or the runner host reserve. `sitecustomize` and
// `usercustomize` auto-run at interpreter startup; `host` is the runner's own
// host script. Even though shared modules ride at the END of sys.path (so they
// cannot shadow the stdlib and cannot auto-run — host.py, finding #11), these
// names are refused at admission so a mistake is legible instead of silently
// inert or confusing.
var reservedPyModules = map[string]bool{
	"sitecustomize": true,
	"usercustomize": true,
	"host":          true,
}

// stdlibPyModules is the set of Python standard-library top-level module names
// (from sys.stdlib_module_names, 3.12). A shared `.py` module may not take one
// of these base names: `json.py`, `os.py`, `ssl.py` would confuse a reader and,
// absent the sys.path-append discipline, would shadow the host's serializer
// (finding #11). Names starting with `_` are excluded — a bundle base name is
// never a private stdlib module.
var stdlibPyModules = func() map[string]bool {
	const names = "abc aifc antigravity argparse array ast asyncio atexit audioop base64 bdb binascii bisect builtins bz2 cProfile calendar cgi cgitb chunk cmath cmd code codecs codeop collections colorsys compileall concurrent configparser contextlib contextvars copy copyreg crypt csv ctypes curses dataclasses datetime dbm decimal difflib dis doctest email encodings ensurepip enum errno faulthandler fcntl filecmp fileinput fnmatch fractions ftplib functools gc genericpath getopt getpass gettext glob graphlib grp gzip hashlib heapq hmac html http idlelib imaplib imghdr importlib inspect io ipaddress itertools json keyword lib2to3 linecache locale logging lzma mailbox mailcap marshal math mimetypes mmap modulefinder msilib msvcrt multiprocessing netrc nis nntplib nt ntpath nturl2path numbers opcode operator optparse os ossaudiodev pathlib pdb pickle pickletools pipes pkgutil platform plistlib poplib posix posixpath pprint profile pstats pty pwd py_compile pyclbr pydoc pydoc_data pyexpat queue quopri random re readline reprlib resource rlcompleter runpy sched secrets select selectors shelve shlex shutil signal site smtplib sndhdr socket socketserver spwd sqlite3 sre_compile sre_constants sre_parse ssl stat statistics string stringprep struct subprocess sunau symtable sys sysconfig syslog tabnanny tarfile telnetlib tempfile termios textwrap this threading time timeit tkinter token tokenize tomllib trace traceback tracemalloc tty turtle turtledemo types typing unicodedata unittest urllib uu uuid venv warnings wave weakref webbrowser winreg winsound wsgiref xdrlib xml xmlrpc zipapp zipfile zipimport zlib zoneinfo"
	m := map[string]bool{}
	for _, n := range strings.Fields(names) {
		m[n] = true
	}
	return m
}()

// ValidBundleAuthority reports whether an authority name is a legal owned
// EXTENSION authority: any legal DNS-style authority that is not the bare
// org-domain label a vocabulary bundle owns. The two shapes stay disjoint, so a
// bundle document is still always exactly one of the two — but which one is
// decided by the shape that grants (vocabulary), never by a category label.
//
// The first label must be a plain word: it becomes the bundle's name, its
// actor and its GraphQL prefix, none of which admit a hyphen.
func ValidBundleAuthority(authority string) bool {
	return ValidAuthority(authority) && !ValidVocabularyAuthority(authority) &&
		ValidName(leadingLabel(authority))
}

// ValidVocabularyAuthority reports whether an authority name is a legal owned
// VOCABULARY authority: one bare lowercase label under the org domain —
// "people.substrate.reamde.dev", "media.substrate.reamde.dev". This is the one
// authority shape the loader checks, because it is the one that grants: an
// authority passing it is built `builtin`. Any extra label fails it
// ("google.bundles" is not one label), so the two owned-authority shapes are
// disjoint and a bundle document is always exactly one of the two.
func ValidVocabularyAuthority(authority string) bool {
	label, ok := strings.CutSuffix(authority, OrgDomainSuffix)
	return ok && ValidName(label)
}

// BundleName is the bundle name an owned authority implies — its FIRST LABEL
// ("google" of "google.bundles.substrate.reamde.dev", "llm" of
// "llm.examples.substrate.reamde.dev", "people" of "people.substrate.reamde.dev").
// It is the bundle's `metadata.id` suffix and the actor an install writes under
// (`bundle:<name>`), and it is what bundleNameProblems holds unique now that
// the rest of the authority is free.
func BundleName(authority string) string {
	return leadingLabel(authority)
}

// VocabularyBundleAuthorities names the authorities in a document stream that
// a VOCABULARY bundle owns — a bundle document on a bare org-domain authority.
// The source an authority is BUILT with follows from it (load.go): shipped
// vocabulary stays `builtin` however it was delivered, so importing
// people.substrate.reamde.dev rather than seeding it does not rename `Person` in GraphQL
// or hand its declarations to a generic API write.
func VocabularyBundleAuthorities(docs []Document) map[string]bool {
	out := map[string]bool{}
	for _, d := range docs {
		if d.Kind != DocBundle {
			continue
		}
		authority := mstr(d.Data, "authority")
		if ValidVocabularyAuthority(authority) {
			out[authority] = true
		}
	}
	return out
}

// buildBundle parses an authority's bundle document (at most one) and checks the
// install closure: `installs:` must name exactly the authority's declared members
// — every kind but the authority header, its actors and the bundle itself. Runs
// after the member kinds parse, inside buildAuthority.
func (l *loader) buildBundle(gd *authorityDocs) {
	g := l.authority
	if len(gd.bundles) == 0 {
		// A TYPO-CATCHER, not the rule: an authority may be spelled any legal
		// way, but one wearing the shipped tree's convention with no bundle
		// document beside it is a closure with no owner, and far more likely a
		// forgotten document than a deliberate name.
		if strings.HasSuffix(g.Name, BundleAuthoritySuffix) {
			l.errf("authority %s: a %q authority is a bundle's owned authority — it must declare a bundle document",
				g.Name, "*"+BundleAuthoritySuffix)
		}
		return
	}
	if len(gd.bundles) > 1 {
		l.errf("%s %s: declared twice", DocBundle, g.Name)
		return
	}
	d := gd.bundles[0]
	where := DocBundle + " " + d.ID
	l.checkKeys(where, d.Data, bundleDataKeys)
	// TWO owned-authority shapes, disjoint by construction: a VOCABULARY
	// bundle owns a bare org-domain authority and ships kinds alone; an
	// EXTENSION owns anything else and may configure, call and connect. Only
	// the first is a name rule — the second is every remaining legal authority,
	// so what is refused here is a malformed authority or a first label that
	// cannot be a bundle name, never a category.
	vocabulary := ValidVocabularyAuthority(g.Name)
	if !ValidBundleAuthority(g.Name) && !vocabulary {
		l.errf("%s: data.authority %q: an authority is DNS-style labels and its FIRST label is the bundle's name, so that label must be one lowercase word",
			where, g.Name)
		return
	}
	name := BundleName(g.Name)
	if d.ID != KindRef(g.Name, name) {
		l.errf("%s: metadata.id must be %q — the authority, then its first label", where, KindRef(g.Name, name))
		return
	}
	b := &Bundle{
		Name:        name,
		Authority:   g.Name,
		Description: l.parseDescription(where+": data", d.Data),
		Vocabulary:  vocabulary,
		Definition:  d.Data,
		SourceYAML:  d.Source,
	}
	l.parseBundleInputs(where, b, d.Data)
	b.Requires = l.parseBundleRequires(where, g.Name, d.Data)
	for i, iv := range mslice(d.Data, "installs") {
		id := fmt.Sprint(iv)
		if !Qualified(id) {
			l.errf("%s: data.installs[%d]: %q — installs names full identities", where, i, id)
			continue
		}
		b.Installs = append(b.Installs, id)
	}
	if len(b.Installs) == 0 {
		l.errf("%s: data.installs is required and non-empty — the exact identities the bundle ships", where)
		return
	}
	b.Modules = l.parseBundleModules(where, d.Data)
	b.OAuth2 = l.parseBundleOAuth2(where, d.Data)
	if vocabulary {
		// Pure vocabulary: kinds, property types, traits and mappings. A
		// callable or a provider flow here would run behind the `builtin`
		// source a bare authority is granted, which is exactly what the
		// bare/extension split exists to prevent.
		if len(b.Inputs) > 0 || len(b.Modules) > 0 || b.OAuth2 != nil || len(g.Functions) > 0 || len(g.Agents) > 0 {
			l.errf("%s: a vocabulary bundle ships kinds and nothing else — no inputs, functions, agents, shared modules or oauth2 block; ship those from an authority carrying a second label, %q",
				where, "<name>"+BundleAuthoritySuffix)
			return
		}
	}
	// The closure: installs and the authority's declared members are the same
	// set, both directions, so an install can never smuggle or orphan a
	// declaration.
	declared := map[string]bool{}
	for _, id := range gd.memberIDs {
		declared[id] = true
	}
	listed := map[string]bool{}
	for _, id := range b.Installs {
		if listed[id] {
			l.errf("%s: data.installs: %q listed twice", where, id)
		}
		listed[id] = true
		if !declared[id] {
			l.errf("%s: data.installs names %q, which the authority does not declare — the closure is the authority", where, id)
		}
	}
	for _, id := range sortedStrings(gd.memberIDs) {
		if !listed[id] {
			l.errf("%s: the authority declares %q, which data.installs does not list — the closure is the authority", where, id)
		}
	}
	g.Bundle = b
}

// bundleInputKeys is one input's key set: the kind whose records satisfy it,
// who consumes it, and what it is for.
var bundleInputKeys = map[string]bool{
	"kind": true, "inject": true, "description": true,
}

// parseBundleInputs reads the optional `inputs:` map — the bundle's declared
// configuration needs, each naming a kind. Names are camelCase; the kind is a
// full identity; `inject` is absent or "functions". Whether the kind RESOLVES
// is resolveBundle's check, against the registry the install admits into.
func (l *loader) parseBundleInputs(where string, b *Bundle, data map[string]any) {
	raw, has := data["inputs"]
	if !has {
		return
	}
	m := asMap(raw)
	if len(m) == 0 {
		l.errf("%s: data.inputs is present but empty — omit it or declare at least one input", where)
		return
	}
	b.Inputs = map[string]BundleInput{}
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	for _, name := range sortedStrings(names) {
		w := fmt.Sprintf("%s: data.inputs[%q]", where, name)
		if !ValidCamel(name) {
			l.errf("%s: an input name must be %s", w, camelRule)
			continue
		}
		im := asMap(m[name])
		if len(im) == 0 {
			l.errf("%s: an input declares at least its kind", w)
			continue
		}
		l.checkKeys(w, im, bundleInputKeys)
		in := BundleInput{
			Kind:        mstr(im, "kind"),
			Inject:      mstr(im, "inject"),
			Description: l.parseDescription(w, im),
		}
		if !Qualified(in.Kind) {
			l.errf("%s.kind: %q — an input names a kind's full identity", w, in.Kind)
			continue
		}
		if in.Inject != "" && in.Inject != BundleInputInjectFunctions {
			l.errf("%s.inject: %q — %q is the one consumer, or omit it for a facility-read input",
				w, in.Inject, BundleInputInjectFunctions)
			continue
		}
		b.Inputs[name] = in
		b.InputOrder = append(b.InputOrder, name)
	}
	if len(b.Inputs) == 0 {
		b.Inputs = nil
	}
}

// parseBundleRequires reads the optional `requires:` list — the AUTHORITIES a
// bundle's closure declares against. Every entry is a DNS authority name, not
// the bundle's own, listed once. The list is intent on the manifest; the
// enforcement is resolveBundle, against the registry the install admits into.
func (l *loader) parseBundleRequires(where, own string, data map[string]any) []string {
	raw, has := data["requires"]
	if !has {
		return nil
	}
	values, isList := raw.([]any)
	if !isList {
		l.errf("%s: data.requires must be a list of authority names", where)
		return nil
	}
	if len(values) == 0 {
		l.errf("%s: data.requires is present but empty — omit it or name at least one authority", where)
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for i, rv := range values {
		authority := strings.TrimSpace(fmt.Sprint(rv))
		switch {
		case !ValidAuthority(authority):
			l.errf("%s: data.requires[%d]: %q must be an authority — a dotted lowercase DNS name, not a kind reference", where, i, authority)
		case authority == own:
			l.errf("%s: data.requires[%d]: %q is the bundle's own authority", where, i, authority)
		case seen[authority]:
			l.errf("%s: data.requires[%d]: %q listed twice", where, i, authority)
		default:
			seen[authority] = true
			out = append(out, authority)
		}
	}
	return out
}

// parseBundleModules reads the optional `modules:` map (filename → inline
// source). Every filename is a bare `.py` or `.go` base name — the bundle
// selects the runtime that imports it — and every source is non-empty and
// within the inline cap. A path separator or `..` is refused: a module is a
// name on the import path, never a way out of the bundle-scoped directory.
func (l *loader) parseBundleModules(where string, data map[string]any) map[string]string {
	raw, has := data["modules"]
	if !has {
		return nil
	}
	m := asMap(raw)
	if len(m) == 0 {
		l.errf("%s: data.modules is present but empty — omit it or ship at least one module", where)
		return nil
	}
	names := make([]string, 0, len(m))
	for k := range m {
		names = append(names, k)
	}
	out := map[string]string{}
	for _, name := range sortedStrings(names) {
		w := fmt.Sprintf("%s: data.modules[%q]", where, name)
		if strings.ContainsAny(name, "/\\") || strings.Contains(name, "..") || name != strings.TrimSpace(name) {
			l.errf("%s: a module filename is a bare base name, no path separators", w)
			continue
		}
		if !strings.HasSuffix(name, ".py") && !strings.HasSuffix(name, ".go") {
			l.errf("%s: a module filename ends in .py or .go — the bundle selects the runtime that imports it", w)
			continue
		}
		// A `.py` module's base name lands on the isolated process's import
		// path: refuse names the interpreter/runner reserve or the stdlib owns,
		// so a `sitecustomize.py`/`json.py`/`host.py` can never desync the
		// protocol host or shadow its serializer (finding #11).
		if base, ok := strings.CutSuffix(name, ".py"); ok {
			lb := strings.ToLower(base)
			if reservedPyModules[lb] {
				l.errf("%s: %q is a reserved module name — it would run or shadow the runner host; rename it", w, name)
				continue
			}
			if stdlibPyModules[lb] {
				l.errf("%s: %q shadows a Python standard-library module — rename it to a bundle-specific name", w, name)
				continue
			}
		}
		src := mstr(m, name)
		if strings.TrimSpace(src) == "" {
			l.errf("%s: source is required — the inline module body", w)
			continue
		}
		if len(src) > ModuleSourceMaxBytes {
			l.errf("%s: source is %d bytes — the inline cap is %d", w, len(src), ModuleSourceMaxBytes)
			continue
		}
		out[name] = src
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// parseBundleOAuth2 reads and validates the optional `oauth2:` block — the
// bundle's trusted provider metadata. Endpoints are https
// absolute URLs (http only to a loopback host); featureScopes maps a toggle
// name to a non-empty list of non-empty scope strings. Absent block → nil,
// and the bundle simply has no host OAuth flow.
func (l *loader) parseBundleOAuth2(where string, data map[string]any) *BundleOAuth2 {
	raw, has := data["oauth2"]
	if !has {
		return nil
	}
	m := asMap(raw)
	w := where + ": data.oauth2"
	if len(m) == 0 {
		l.errf("%s is present but empty — omit it or declare the provider endpoints", w)
		return nil
	}
	l.checkKeys(w, m, oauth2MetaKeys)
	o := &BundleOAuth2{
		ClientInput:           mstr(m, "clientInput"),
		AuthorizationEndpoint: mstr(m, "authorizationEndpoint"),
		TokenEndpoint:         mstr(m, "tokenEndpoint"),
		RevocationEndpoint:    mstr(m, "revocationEndpoint"),
		EmailEndpoint:         mstr(m, "emailEndpoint"),
		EmailProperty:         mstr(m, "emailProperty"),
	}
	if o.ClientInput == "" {
		l.errf("%s.clientInput is required — the input whose resolved record carries the OAuth client credentials", w)
	}
	l.checkOAuthEndpoint(w, "authorizationEndpoint", o.AuthorizationEndpoint, true)
	l.checkOAuthEndpoint(w, "tokenEndpoint", o.TokenEndpoint, true)
	l.checkOAuthEndpoint(w, "revocationEndpoint", o.RevocationEndpoint, false)
	l.checkOAuthEndpoint(w, "emailEndpoint", o.EmailEndpoint, false)
	// emailEndpoint and emailProperty travel together: one without the other is
	// a no-op that reads like a mistake.
	if (o.EmailEndpoint == "") != (o.EmailProperty == "") {
		l.errf("%s: emailEndpoint and emailProperty must be declared together", w)
	}
	if o.EmailProperty != "" && !ValidCamel(o.EmailProperty) {
		l.errf("%s.emailProperty: %q must be %s", w, o.EmailProperty, camelRule)
	}
	if fsRaw, ok := m["featureScopes"]; ok {
		fs := asMap(fsRaw)
		o.FeatureScopes = map[string][]string{}
		toggles := make([]string, 0, len(fs))
		for k := range fs {
			toggles = append(toggles, k)
		}
		for _, toggle := range sortedStrings(toggles) {
			scopes := mslice(fs, toggle)
			if len(scopes) == 0 {
				l.errf("%s.featureScopes[%q]: at least one scope is required", w, toggle)
				continue
			}
			var out []string
			for _, sv := range scopes {
				s := strings.TrimSpace(fmt.Sprint(sv))
				if s == "" {
					l.errf("%s.featureScopes[%q]: a scope must be a non-empty string", w, toggle)
					continue
				}
				out = append(out, s)
			}
			if len(out) > 0 {
				o.FeatureScopes[toggle] = out
			}
		}
	}
	return o
}

// checkOAuthEndpoint admits an https absolute URL — or, so a local test
// provider needs no TLS, an http URL whose host is loopback. A remote http
// endpoint would send client credentials in the clear and is refused.
func (l *loader) checkOAuthEndpoint(where, field, raw string, required bool) {
	if raw == "" {
		if required {
			l.errf("%s.%s is required — the provider's %s (https)", where, field, field)
		}
		return
	}
	u, err := url.Parse(raw)
	if err != nil || !u.IsAbs() || u.Host == "" {
		l.errf("%s.%s: %q is not an absolute URL", where, field, raw)
		return
	}
	switch u.Scheme {
	case "https":
	case "http":
		if !isLoopbackHost(u.Hostname()) {
			l.errf("%s.%s: %q must be https — a remote http endpoint sends credentials in the clear", where, field, raw)
		}
	default:
		l.errf("%s.%s: %q must be an https URL", where, field, raw)
	}
}

// isLoopbackHost reports whether a URL host is the local machine — the only
// place a plaintext-http OAuth endpoint is admitted (a test provider).
func isLoopbackHost(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1" || host == "[::1]"
}

// resolveBundle validates a bundle against the loaded registry, after trait
// bindings resolve: every input's kind exists and is reachable (the bundle's
// own authority, core, or a required one), an injected input's kind is the
// bundle's own (another authority's secrets never cross into its functions),
// and the oauth2 block's clientInput names an oauth2-trait input.
func (r *Registry) resolveBundle(g *Authority) []string {
	b := g.Bundle
	if b == nil {
		return nil
	}
	var problems []string
	where := DocBundle + " " + b.Authority
	// THE REQUIRES CHECK. Vocabulary is imported, not seeded, so a closure
	// that declares against another authority says so and is refused here when
	// that authority is absent — one legible problem naming what to import
	// first, instead of an unresolvable edge target or mapping `to:` deeper in
	// the same admission.
	for _, req := range b.Requires {
		if _, ok := r.AuthorityByName(req); !ok {
			problems = append(problems, fmt.Sprintf(
				"%s: data.requires names %s, which this repository does not have — import that authority's bundle first",
				where, req))
		}
	}
	required := map[string]bool{AuthorityCore: true, g.Name: true}
	for _, req := range b.Requires {
		required[req] = true
	}
	for _, name := range b.InputOrder {
		in := b.Inputs[name]
		w := fmt.Sprintf("%s: data.inputs[%q]", where, name)
		ik, ok := r.ByIdentity(in.Kind)
		switch {
		case !ok:
			problems = append(problems, fmt.Sprintf("%s.kind: unknown kind %q", w, in.Kind))
			continue
		case !required[ik.Authority]:
			problems = append(problems, fmt.Sprintf("%s.kind: %q is declared in %s — an input's kind lives in the bundle's own authority, core, or an authority the bundle requires",
				w, in.Kind, ik.Authority))
		}
		// An injected input's records cross into the bundle's function
		// invocations, secrets resolved. Only the bundle's own records may:
		// injecting another authority's kind would hand this bundle's
		// functions secrets their owner never addressed to it.
		if in.Inject == BundleInputInjectFunctions && ik.Authority != g.Name {
			problems = append(problems, fmt.Sprintf("%s: an injected input's kind must be the bundle's own — %q is declared in %s",
				w, in.Kind, ik.Authority))
		}
	}
	// The oauth2 trait and the oauth2 block travel together, both directions:
	// a client kind without trusted endpoints can never serve the facility,
	// and two client kinds would leave the flow no one record to read.
	for _, tn := range g.KindOrder {
		t := g.Kinds[tn]
		if !t.Implements(TraitOAuth2Core) {
			continue
		}
		switch {
		case b.OAuth2 == nil:
			problems = append(problems, fmt.Sprintf("%s: type %s implements the %s trait but the bundle ships no data.oauth2 block — the provider endpoints are manifest metadata, not record properties",
				where, t.Identity, TraitOAuth2))
		case b.Inputs[b.OAuth2.ClientInput].Kind != t.Identity:
			problems = append(problems, fmt.Sprintf("%s: type %s also implements %s — one bundle carries one client kind, the declared clientInput's",
				where, t.Identity, TraitOAuth2))
		}
	}
	if b.OAuth2 != nil {
		// The client credentials live on an input's resolved record; the
		// endpoints stay manifest metadata. The named input's kind implements
		// the oauth2 trait so the facility finds clientId/clientSecret where
		// the trait contract puts them — and it is the bundle's OWN kind,
		// always: the facility opens the resolved record's sealed
		// clientSecret and sends it to THIS bundle's declared token endpoint,
		// so a client input naming another authority's kind would let one
		// bundle exfiltrate another's client secret through its own
		// attacker-chosen endpoints.
		ci, ok := b.Inputs[b.OAuth2.ClientInput]
		ck, ckExists := (*Kind)(nil), false
		if ok {
			ck, ckExists = r.ByIdentity(ci.Kind)
		}
		switch {
		case !ok:
			problems = append(problems, fmt.Sprintf("%s: data.oauth2.clientInput %q names no declared input", where, b.OAuth2.ClientInput))
		case ckExists && ck.Authority != g.Name:
			problems = append(problems, fmt.Sprintf("%s: data.oauth2.clientInput %q: its kind %q is declared in %s — the client kind is the bundle's own, so one bundle's flow can never open another's client secret",
				where, b.OAuth2.ClientInput, ci.Kind, ck.Authority))
		case ckExists && !ck.Implements(TraitOAuth2Core):
			problems = append(problems, fmt.Sprintf("%s: data.oauth2.clientInput %q: its kind %q does not implement the %s trait (%s — a same-named local trait does not count)",
				where, b.OAuth2.ClientInput, ci.Kind, TraitOAuth2, TraitOAuth2Core))
		case ckExists && ck.Implements(TraitAccountConfigCore):
			// A client is not a Connection: purge tears accounts down first
			// (revocation reads the still-live client), so one kind wearing
			// both hats would have no consistent teardown order.
			problems = append(problems, fmt.Sprintf("%s: data.oauth2.clientInput %q: its kind %q also implements the %s trait — the client kind and an account kind are never one",
				where, b.OAuth2.ClientInput, ci.Kind, TraitAccountConfig))
		}
		// Every featureScopes key names a bool toggle the bundle declares:
		// StartOAuth reads it off the account row to decide whether its scopes
		// are requested.
		for _, toggle := range sortedStrings(mapKeys(b.OAuth2.FeatureScopes)) {
			if !g.hasBoolToggle(toggle) {
				problems = append(problems, fmt.Sprintf("%s: data.oauth2.featureScopes[%q] names no bool property on any of the bundle's types",
					where, toggle))
			}
		}
		// emailProperty must name a writer: oauth property on some bundle type —
		// the facility writes it as its own actor, and only a writer: oauth slot
		// keeps the owner out of it.
		if name := b.OAuth2.EmailProperty; name != "" && !g.hasOAuthWritableProp(name) {
			problems = append(problems, fmt.Sprintf("%s: data.oauth2.emailProperty %q names no `writer: oauth` property on any of the bundle's types",
				where, name))
		}
	}
	return problems
}

// hasBoolToggle reports whether some type in the authority declares name as a bool
// property — the feature toggle StartOAuth reads off the account row.
func (g *Authority) hasBoolToggle(name string) bool {
	for _, tn := range g.KindOrder {
		if p, ok := g.Kinds[tn].Prop(name); ok && p.Datatype == DatatypeBool {
			return true
		}
	}
	return false
}

// hasOAuthWritableProp reports whether some type in the authority declares name as
// a `writer: oauth` property — the slot the facility writes the derived email
// into, unreachable to the owner.
func (g *Authority) hasOAuthWritableProp(name string) bool {
	for _, tn := range g.KindOrder {
		if p, ok := g.Kinds[tn].Prop(name); ok && p.Writer == WriterOAuth {
			return true
		}
	}
	return false
}

func mapKeys(m map[string][]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// Bundles lists every loaded bundle, ordered by authority.
func (r *Registry) Bundles() []*Bundle {
	var out []*Bundle
	for _, g := range r.AuthorityList() {
		if g.Bundle != nil {
			out = append(out, g.Bundle)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Authority < out[j].Authority })
	return out
}

// BundleOf returns the bundle owning an authority, if the authority is a bundle's.
func (r *Registry) BundleOf(authority string) (*Bundle, bool) {
	g, ok := r.AuthorityByName(authority)
	if !ok || g.Bundle == nil {
		return nil, false
	}
	return g.Bundle, true
}

func sortedStrings(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}
