# Terms

One word per thing. If a word is not here, it is not a term — and where these
pages and the code disagree, the code is right.

Dead words, and what replaced them: **entity** → record, **group** → authority,
**type** → kind (or *plural*, for the path segment), **capability** → trait,
**schema** → vocabulary, **log** → changelog, **extension** → bundle,
**relationship** → edge, **tenant** and **identity** → nothing, there are none.

## The shape of things

| Term | What it is |
| ---- | ---------- |
| **repository** | Everything one user has: one changelog, the records folded out of it, and the blob store beside them. One user, one repository, no sharing. |
| **record** | One typed thing. Identity is `(kind, id)` within a repository. It is the only thing the substrate stores. |
| **kind** | What a record is, written `<authority>/<name>` — or a bare `<name>` for a kind local to one repository. A kind declares the properties and edges its records may carry. |
| **authority** | The DNS-style label that publishes a set of kinds and decides who may write their declarations. One path segment: `/api/v1/{authority}/{plural}`. |
| **plural** | A kind's collection segment in a path — `people` for `people.substrate.reamde.dev/person`. |
| **property** | A named, typed value on a record, declared by its kind. |
| **property type** | A named refinement of a base type plus its validations, declared by an authority and reusable across its kinds. |
| **trait** | A contract a kind implements: a set of properties a kind promises to declare, so unrelated kinds can be treated alike. |
| **edge** | A named, directed, traversable link from one record to another — the only way one record points at another. Its name is its `rel`. |
| **reference** | A `{kind, id}` pointer stored as an ordinary property value. Data, not a traversable edge. |

## Truth and derivation

| Term | What it is |
| ---- | ---------- |
| **changelog** | The repository's one append-only, strictly sequential list of every change. It is the truth. |
| **fold** | The records the changelog is replayed into. The only write path to them, so a live write and a rebuild cannot drift. |
| **projection** | Recomputing a record's mapped properties from every live source record that maps onto it. |
| **recordmapping** | A declaration of how one kind's properties reach the record its edge points at. |
| **merge** | Joining two records of one kind so the winner absorbs the loser's place in the graph. |
| **split** | The undo of exactly one recorded merge. |
| **tier** | A property manager's standing against recompute: `owner` > `bundle` > `machine`. Recompute overwrites only machine-held properties. |
| **actor** | What wrote a property or record. Attribution, never authorization. |

## Declarations

| Term | What it is |
| ---- | ---------- |
| **vocabulary** | Everything declarable, held as ordinary records: authorities, kinds, property types, traits, recordmappings, functions, agents, bundles and actors. Applied through `POST /{core}/vocabulary/apply`. |
| **registry** | The live, in-memory vocabulary a repository rebuilds from its own stored declarations when it opens. |
| **dialect** | A monotonic integer stamped on each repository naming the shape its stored declarations speak. A binary older than the stamp refuses to open it. |

## Installed things

| Term | What it is |
| ---- | ---------- |
| **bundle** | The unit of installation: a closure of declarations and behavior, applied and removed as one unit, owning exactly one authority. |
| **integration** | A bundle whose job includes an ongoing connection to an outside provider. A catalog facet of a bundle, not a different thing. |
| **vocabulary bundle** | A bundle that ships only kinds and rules — no functions, no provider. |
| **account** | One configured connection to a provider: a record of an `accountconfig`-trait kind. The console groups these under **Connections**. |
| **catalog** | The read-only list of the bundle closures built into the binary. A source to install from, never an authority. |
| **callable** | The union of function and agent — what a trigger binds and what dispatch invokes. |
| **function** | A callable whose body is inline Python or Go, bounded by a declared `capabilities` envelope. |
| **agent** | A callable whose body is an LLM loop. Alpha. |
| **llmprovider** | One place an agent buys completions: a wire, an endpoint and a key, as data. Alpha. |
| **wire** | The protocol an `llmprovider`'s adapter speaks — `openai`, `anthropic` or `azure` — never a company: a gateway that speaks OpenAI's wire is an `openai` row. |
| **model** | The model id an agent sends on every completion, a plain string its provider understands. There is no model record and no tier. |
| **trigger** | One delivery binding: exactly one source (`record`, `schedule` or `webhook`) to one callable. |
| **run** | One settled delivery attempt of a trigger. |

## The wire

| Term | What it is |
| ---- | ---------- |
| **token** | A bearer credential, itself a record. It has full access to its repository; it carries no scopes. |
| **blob** | Content-addressed bytes whose digest is its id and whose manifest is an ordinary record. |
| **watch** | The ndjson tail of a collection or of the changelog, resumable from a cursor. |
| **feature** | A named, stability-stamped entry in `GET /api`, so a client reads what a deployment offers instead of probing for failures. |

## Words that mean something narrower than they look

- **capabilities** — only ever a function's declared security envelope. The
  substrate does not otherwise use the word; what a deployment offers is a
  *feature*, and what a kind promises is a *trait*.
- **schema** — only GraphQL's own schema, JSON Schema for a function's input
  and output, and Postgres. The substrate's declarations are its *vocabulary*.
- **log** — only logging. What holds the deltas is the *changelog*.
- **extension** — only a file extension or a Postgres extension. What you
  install is a *bundle*.
