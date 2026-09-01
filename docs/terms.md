# Terms

One word per thing. If a word is not here, it is not a term — and where these
pages and the code disagree, the code is right.

Dead words, and what replaced them: **entity** → record, **group** → authority,
**type** → kind, **capability** → trait,
**schema** → vocabulary, **log** → changelog, **extension** → bundle,
**relationship** and **edge** → reference, **plural** → the kind's name, which
is the collection segment (decision 0033), **tenant** and **identity** →
nothing, there are none.

## The shape of things

| Term | What it is |
| ---- | ---------- |
| **repository** | Everything one user has: one changelog, the records folded out of it, and the blob store beside them. One user, one repository, no sharing. |
| **record** | One typed thing. Identity is `(kind, id)` within a repository. It is the only thing the substrate stores. |
| **kind** | What a record is, written `<authority>/<name>`; every kind carries an authority. A kind declares the properties its records may carry. |
| **authority** | The DNS-style label that publishes a set of kinds and decides who may write their declarations. One path segment: `/api/v1/{authority}/{kind}`. |
| **property** | A named, typed value on a record, declared by its kind. |
| **property type** | A named refinement of a base type plus its validations, declared by an authority and reusable across its kinds. |
| **trait** | A contract a kind implements: a set of properties a kind promises to declare, so unrelated kinds can be treated alike. |
| **reference** | A named, directed pointer at one record, declared as a property and stored as the target's `<kind>/<id>` path. The only link between records; it may declare properties of its own, carried beside the path. |

## Truth and derivation

| Term | What it is |
| ---- | ---------- |
| **changelog** | The repository's one append-only, strictly sequential list of every change. It is the truth. |
| **fold** | The records the changelog is replayed into. The only write path to them, so a live write and a rebuild cannot drift. |
| **projection** | Recomputing a record's mapped properties from every live source record that maps onto it. |
| **recordmapping** | A declaration of how one kind's properties reach the record its subject reference points at. |
| **merge** | Joining two records of one kind so the winner absorbs the loser's place in the graph. |
| **split** | The undo of exactly one recorded merge. |
| **tier** | A property manager's standing against recompute: `owner` > `bundle` > `machine`. Recompute overwrites only machine-held properties. |
| **actor** | What wrote a property or record. Attribution, never authorization. |

## Declarations

| Term | What it is |
| ---- | ---------- |
| **vocabulary** | Everything declarable, held as ordinary records: authorities, kinds, property types, traits, recordmappings, functions, agents, bundles and actors. Applied through `POST /{core}/vocabulary/apply`. |
| **registry** | The live, in-memory vocabulary a repository rebuilds from its own stored declarations when it opens. |
| **dialect** | A monotonic integer stamped on each repository naming a shape it speaks. A binary older than the stamp refuses to open the repository. There are two, refused the same way and stamped differently: the **vocabulary dialect** over its stored declarations, stamped at open by the promotion that rewrites them, and the **changelog dialect** over the entries in its changelog, claimed by the transaction that appends them. |
| **managed** | A declared property the ENGINE stamps (`managed: true`): a declaration's `version`, its `source`, the quarantine marks, a bundle's lifecycle bools, and the decision stamps `decidedAt` and `resolvedAt`. A write may echo the stored value, but a different one is refused rather than dropped, and a client renders it read-only. The one exception is a declaration's `version`, which the engine resolves itself: an incoming value is honored only when it moves past the stored one, and anything else (absent, echoed, lower) is stamped server-side. Not projection's *managed properties*, which is the ownership rule over a record's values. |

## Installed things

| Term | What it is |
| ---- | ---------- |
| **bundle** | The unit of installation: a closure of declarations and behavior, applied and removed as one unit, owning exactly one authority. |
| **integration** | A bundle whose job includes an ongoing connection to an outside provider. A catalog facet of a bundle, not a different thing. |
| **vocabulary bundle** | A bundle that ships only kinds and rules — no functions, no provider. |
| **input** | A bundle's named configuration need: it names a kind, and the engine resolves ONE record per input — the bound record, else the record whose id is `default`, else the sole live record, else nothing, surfaced per input on the bundle's status. No cardinality is enforced on the kind. |
| **bind** | The explicit step of input resolution: a reference on the bundle's own record row, named for the input, pointing it at a chosen record. `POST /core.substrate.reamde.dev/bundle/{id}/bind`; an empty record unbinds. |
| **account** | One configured connection to a provider: a record of an `accountconfig`-trait kind. The console groups these under **Connections**. |
| **catalog** | The read-only list of the bundle closures built into the binary. A source to install from, never an authority. |
| **callable** | The union of function and agent — what a trigger binds and what dispatch invokes. |
| **function** | A callable whose body is inline Python or Go, bounded by its declared `permissions`: `reads`, `writes`, `call`, `network` and `mutations`, five grants in one object on the declaration. |
| **agent** | A callable whose body is an LLM loop. Alpha. |
| **llmprovider** | One place an agent buys completions: a wire, an endpoint and a key, as data. Alpha. |
| **wire** | The protocol an `llmprovider`'s adapter speaks — `openai`, `anthropic` or `azure` — never a company: a gateway that speaks OpenAI's wire is an `openai` row. |
| **model** | The model id an agent sends on every completion, a plain string its provider understands. There is no model record and no tier. |
| **trigger** | One delivery binding: exactly one source (`record`, `schedule` or `webhook`) to one callable. |
| **run** | One settled delivery attempt of a trigger. |

## Sensitive values

| Term | What it is |
| ---- | ---------- |
| **sensitive** | The umbrella over `secret` and `digest`: redacted on every read surface, excluded from search, filtering, ordering, titles and change payloads. |
| **secret** | Confidential material as a property. The record and the changelog store only an opaque ref; the material lives encrypted in the sealed store, resolved only by the host reads that spend it. Rotation deletes the old material. |
| **digest** | A one-way SHA-256 the server minted to compare, never to reveal (a token's `hash`). Redacted like a secret but stored as the value itself: auth matches it in SQL. |
| **sealed store** | The engine table holding secret material (property secrets, OAuth tokens, the password hash, the TOTP seed) encrypted under the repository's DEK, addressed by refs. |
| **DEK** | The repository's own data-encryption key. Wrapped twice: under the host credential key in the control plane (live operation) and to the user's age recipient in the `recoverykey` record (recovery). |
| **recovery key** | The age identity the user keeps and the substrate never stores. It opens the `recoverykey` record's wrap, so a backup plus the identity is a complete recovery with no host key. Enrolled at registration, or once via `recovery enroll`. |
| **reseal** | The operator migration that moves legacy secret values into the sealed store, re-points records and changelog at the refs, and re-keys payloads under the DEK. The one sanctioned, values-only rewrite of history. |

## The wire

| Term | What it is |
| ---- | ---------- |
| **token** | A bearer credential, itself a record. It has full access to its repository; it carries no scopes. |
| **blob** | Content-addressed bytes whose digest is its id and whose manifest is an ordinary record. |
| **watch** | The ndjson tail of a collection or of the changelog, resumable from a cursor. |
| **feature** | A named entry in `GET /.well-known/substrate/server.json` carrying its stability and the surfaces that serve it (`rest`, `graphql`, or both), so a client reads what a deployment offers instead of probing for failures. |

## Words that mean something narrower than they look

- **capabilities** — only ever the name for a function's security envelope, and
  no longer a key: the grant is five keys on the declaration itself, and a
  document nesting them under `capabilities:` is refused. The substrate does not
  otherwise use the word; what a deployment offers is a *feature*, and what a
  kind promises is a *trait*.
- **schema** — only GraphQL's own schema, the JSON Schema a function's
  `arguments`/`returns` compile into, and Postgres. The substrate's declarations
  are its *vocabulary*.
- **log** — only logging. What holds the deltas is the *changelog*.
- **extension** — only a file extension or a Postgres extension. What you
  install is a *bundle*.
