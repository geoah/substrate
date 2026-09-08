---
status: accepted
date: 2026-09-08
decision-makers: George Antoniadis
---

# 0058. A non-core kind's GraphQL name always carries its full authority

## Context and Problem Statement

[0047](0047-a-kind-lives-in-a-package.md) named an installed kind
`<Package>_<Kind>` and joined the full authority only when two authorities
installed a package of one word, for every kind of both packages. That second
install renames every type of the first package, the generated reference
object types included, and a query written
against `Tasks_Task` fails once `acme.example.com/tasks` lands
([#382](https://github.com/geoah/substrate/issues/382)). The live path is a
repository declaring `<user>.<host>/google/...` and then installing the
`google` provider. The vocabulary tests pinned the rename as intended, so this
record reverses 0047's naming paragraph.
[0053](0053-rest-is-supported-all-of-graphql-is-preview.md) made every part
of GraphQL a preview, so the rename this costs is announced, not a v1 wire
break.

## Considered Options

- Always prefix every non-core kind with its full authority, folded
  injectively (a dot to `_`, a hyphen to `__`, a `_` lead before a digit), so
  a name is a function of the kind alone and two authorities never share one
- Always prefix, folding the authority lossily: a dot to `_` and every other
  character a GraphQL name may not carry dropped, with the collision refused
  at declaration
- Refuse at install a package word already held by another authority in the
  repository, and keep `<Package>_<Kind>`
- Prefix only the newcomer, so the first package keeps its name
- A persisted per-repository alias table that pins each kind's name

## Decision Outcome

Chosen: always prefix, with the injective fold. A non-core kind's GraphQL
name is `<Authority>_<Package>_<Kind>`, each segment with its first letter
upper-cased: `ada.example.com/tasks/task` is `Ada_example_com_Tasks_Task`,
`providers.substrate.reamde.dev/google/person` is
`Providers_substrate_reamde_dev_Google_Person`. Only the seeded `core` package
keeps the bare singular (`Token`), and so does a bare reference with no
authority. The authority fold is exactly this: `.` becomes `_`, `-` becomes
`__`, and an authority whose first character is a digit gains a leading `_`
(`3rd.example.com` is `_3rd_example_com`). An authority is lowercase labels
of letters, digits and inner hyphens joined by dots and never carries an
underscore, so the fold reads back unambiguously (`my-host.example.com` is
`My__host_example_com`, `myhost.example.com` is `Myhost_example_com`), and
every result satisfies GraphQL's `^[_a-zA-Z][_a-zA-Z0-9]*$`. The package and
the kind are single words and need no fold. The server owns the rule and the
console mirrors it by hand. The declaration-time refusal of two kinds spelling
one name stands, naming both references in full, and it also refuses a
produced name that fails that identifier pattern; nothing is renamed silently.

This supersedes 0047's GraphQL naming paragraph and the `Tasks_Task` and
`People_Person` spellings its Consequences give as examples; 0047 stays
accepted for everything else it decides.

The name depends on nothing but the kind, which is the property the other
options lack or pay for. The lossy fold lost because it drops the hyphen, so
`my-host.example.com` and `myhost.example.com` spell one name and the second
of two legal authorities is refused for a collision it never made in its own
reference, and because a digit-first authority folded to an illegal name that
took the whole repository's schema down. Refusing the second same-word package blocks a user
from installing a provider until they rename their own package, for a preview
surface. Prefixing only the newcomer makes a name depend on install order, the
order dependence 0047 rejected, and needs stored state to remember who came
first. An alias table is that state made explicit: a record that must survive
rebuild and export, for a surface 0053 says may change.

### Consequences

- Good, because installing a package cannot rename an existing kind's type, so
  a query written today keeps working after every later install.
- Good, because there is no tie-break branch, no install refusal and no
  persisted state: the rule is one function of the reference and the source.
- Good, because two authorities sharing a first label (`acme.example.com`,
  `acme.example.org`) still get two names, so 0014's first-label reservation
  stays discharged, and two authorities differing only by a hyphen get two
  names as well, so no legal authority is ever refused for another's name.
- Bad, because every non-core type is renamed once, in every repository, with
  no compatibility window: `Tasks_Task` becomes `Ada_example_com_Tasks_Task`,
  and nothing rewrites a client's old queries. GraphQL is preview (0053), so
  the rename ships announced, as a preview change and not a wire break.
- Bad, because every generated name is long, and a sample imported under a
  long authority is longer still; a hyphen reads as a double underscore and a
  digit-first authority as a leading underscore, neither of which a reader
  would guess without this record.

### Confirmation

`TestGraphQLNamesAlwaysCarryTheAuthority`,
`TestAuthoritiesFoldToDistinctGraphQLNames` and
`TestAPublishedKindIsNamedLikeAnInstalledOne` (`internal/vocabulary`) pin the
fold, the hyphen, digit-first and bare-reference cases, and that three
authorities publishing one package word all load;
`TestGraphQLInstallingASameWordPackageKeepsExistingNames` (`internal/api`)
runs a query on an installed kind's type before and after a second authority
installs the same package word;
`TestGraphQLDigitFirstAndHyphenatedAuthoritiesBuild` pins that the schema
builds for those authorities; `TestGraphQLNamesDoNotDependOnRegistryOrder`
pins the bare core name. The console's `graphqlTypeName` test holds the mirror
to the same spellings, and the e2e suite's widget fixture names the generated
type in full.

## More Information

Amends the GraphQL naming paragraph of
[0047](0047-a-kind-lives-in-a-package.md): the package segment, the actor
grammar and the rest of that record stand. Revisit if kind identity moves to
URLs, which would change what an authority folds to, or if a client outside
the tree needs shorter names, which would be a new record on aliases.
