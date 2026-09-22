# The Notion cut — what it proves, and what it does not

26 recordings, a CUT of the owner's real Notion workspace, selected OFFLINE by
what the payloads CONTAIN and then pseudonymised, repaired and audited to zero.
Nothing here is synthetic.

    python3 tools/rawpull/notion.py --cut /tmp/notion-cut
    python3 tools/pseudonymise.py /tmp/notion-cut providers/notion/fixtures --provider notion
    python3 tools/rawpull/notion.py --repair providers/notion/fixtures
    python3 providers/notion/fixtures/audit.py        # MUST print 0

## What it holds

| | count |
| --- | --- |
| pages | 12 |
| data sources | 2 |
| databases | 2 |
| users | 9 — 2 people, 7 bots |
| blocks | 577, across 21 block recordings |
| block recordings whose list PAGES (`has_more` + `next_cursor`) | 9 |

## What it proves, case by case

| case | what landed |
| --- | --- |
| a page that is a database ROW | 2, both under one data source, `parent.type: data_source_id` carrying BOTH the data source id and the database id |
| a page under another page | 9 |
| a page at the top of the workspace | 1 — the index page, whose real block list is 152 pages long and is kept at 2 |
| a data source with its property SCHEMA | 2 — `url`, `rich_text`, `select` (19 options), `multi_select`, `number`, `checkbox`, `title` |
| the database behind a data source | 2, reached only by the addressed read a data source's `parent` asks for — search returns no database at all |
| a database naming its data sources | 2, as repeated references |
| page property VALUES | `title` 12, `select` 8, `multi_select` 5, `rich_text` 4, `url` 3, `checkbox` 2, `number` 2, `date` 1 |
| a block list that PAGES | 9 recordings carry `has_more` with a `next_cursor` that names the next recording |
| a block tree with children | the index page's `child_page` blocks, 60 of them |
| block types | `quote` 427, `heading_2` 77, `child_page` 60, `heading_1` 9, `paragraph` 3, `image` 1 |
| a person with an address | 2 users of `type: person`, both carrying `person.email` — which is what a person mapping probes |
| a bot, including the integration the token IS | 7, one of them the `users/me` bot carrying the workspace's name, id and upload limit |
| a page with an icon | the data sources carry an `emoji` icon |

## What it does NOT cover, and which of them is a pull gap

A green run over a set that cannot reach a case proves nothing about that
case. Named here rather than implied:

* **28 of the 34 declared block types.** The owner's workspace is a meeting
  transcript archive: `quote`, `heading_*`, `paragraph`, `child_page` and one
  `image` is the whole of it. `to_do`, `code`, `table` / `table_row`,
  `callout`, `toggle`, `bulleted_list_item`, `numbered_list_item`,
  `column_list` / `column`, `synced_block`, `bookmark`, `embed`,
  `link_preview`, `link_to_page`, `equation`, `divider`, `breadcrumb`,
  `table_of_contents`, `file`, `pdf`, `video`, `audio`, `template`,
  `transcription` and `unsupported` are DECLARED from the API reference and
  exercised by nothing. That includes the two payloads that carry REFERENCES
  — `synced_block.synced_from.block_id` and `link_to_page` — so the one part
  of the block union with a relation in it is declared and unproven.
* **A rich-text `mention` or `equation` span, an annotation, and an `href`.**
  Zero in 27,493 real spans: every span in the owner's corpus is a plain
  `text` with default annotations and no link. So the `json` shape is proven
  and its two other variants are not.
* **A `people` or a `relation` property value.** Zero in 16,979 objects. This
  is why neither is decoded into a reference (README, *What could not be
  expressed*; T-039).
* **A comment.** The owner's integration answers `403 restricted_resource` on
  `GET /v1/comments` — it was created without the **read comments**
  capability. This is a PULL GAP with an owner action behind it, not a shape
  the API lacks: T-038 says exactly what to grant and what to re-run.
* **A trashed page.** `in_trash` is false on all 16,979 objects, because
  `/v1/search` never returns one. Deletion reconciliation is therefore
  untested and unimplemented (T-040).
* **A second data source on one database.** Both databases here have exactly
  one, so the case that MOTIVATES the 2025-09-03 pin (a database that grows a
  second source disappears under 2022-06-28) is argued from Notion's own
  changelog rather than demonstrated by a fixture.
* **A 429, a 500, or an expired cursor.** The mock's fault matrix
  (`tools/mockserver.py --faults`) can inject all three; this provider ships
  no `faults.json` yet, so the rate-limit and permanent-failure paths in
  `notionsync.py` are read, not run. Same gap as Google's failure matrix.
* **A cover image, and a custom emoji icon.** No object in the cut carries
  either; an `emoji` icon it does. The custom emoji is why no `customemoji`
  kind is declared (`docs/reviews/notion-codex.md`, finding 6).
* **A database `created_by` / `last_edited_by`.** Documented by the reference,
  carried by no payload in the owner's pull — declared and extracted anyway,
  and named here rather than claimed proven.
* **A second WORKSPACE.** The owner has two Notion integrations (a second
  workspace under a different token). The cut is one workspace, because one
  token addresses one workspace and two `users/me` responses cannot share a
  recording name. A second account would want `raw/notion/<account>/`, which
  is the layout `provider-practices.md` §7 already describes for Google.

## The raw pull behind the cut is bounded too

The FIXTURES are complete — `--cut` refuses to cut a page whose block tree was
never recorded, and the e2e proves every request hits an exact recording. The
PULL they are cut from is not: it walks the block trees of its first
`--pages` pages, 81 of the workspace's 16,977. That bounds the SEED, not this
set, and it is measured rather than estimated:

    $ python3 tools/rawpull/notion.py --coverage
      17,331 requests replayed, 435 resolved, 16,896 MISSING (blocks)

**T-051** holds the command that closes it and what it costs.

## What survives the pseudonymiser, and why

| moves | stays |
| --- | --- |
| every rich-text span, both halves (`text.content` and `plain_text`), replaced with generated prose of the same shape | every `next_cursor` and `start_cursor` — the pagination contract, and the thing a recording's NAME is derived from |
| every USER UUID, through the persona's salted `hexid`, so it stays a UUID | every PAGE, BLOCK, DATABASE and DATA SOURCE UUID — a content id that names no person, and the recording-name contract (`GET_v1_blocks_<id>_children…`) |
| every `properties` KEY and the `name` beside it, through one bucket so the two still agree | every property id (`%3C%3BXv`), every `type` discriminator, every colour |
| every select / multi-select option name | every instant, every boolean, every number |
| every page, database and data source `url` — its path IS the title — re-derived from the already-fake title | `app.notion.com`, put back by `--repair` because `notion.com` is not in `persona.KEEP_HOSTS` |
| every workspace name and workspace UUID | Notion's own asset hosts and its response shapes, byte for byte otherwise |
| the QUERY STRING of every non-Notion URL — a `?cid=` names a real place | a vendor host the pseudonymiser keeps on purpose |

`fixtures/cast.local.json` does not exist for this provider: which real pages
were cut is derivable from `raw/notion` and `tools/persona.local.json`, both
gitignored, and naming them here would undo the substitution for anybody
reading the repository (`provider-practices.md` §9).
