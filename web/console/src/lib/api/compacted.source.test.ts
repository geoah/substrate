/** Every place the console's source can put a continuation on the wire is a
 * reader under RULE W (compacted.ts) or sends none. The source is read at
 * test time, tests aside: every use of a function that sends a continuation
 * (a list's `after`, a history page's `before` with its `generation`, a
 * watch's `from` with its `generation`, the time seek's probes) and every
 * `getNextPageParam` (an infinite query paged with Load more) must be in
 * READERS, by file and by the function used, one classification per use. A
 * use in a file the table does not list, a count that moved and an entry
 * that no longer exists each fail, so a new reader is classified the day it
 * lands, and each classification must match where the use sits (`fits`).
 * A sender is found by the name it was imported under (`as`) and as a
 * namespace import's member, and one reached by a computed name or exported
 * under another is refused. Beside it, the continuation's parameters are
 * written only inside those functions, so nothing sends one around them; a
 * parameter whose name is computed counts as one. */

import ts from "typescript"
import { describe, expect, it } from "vitest"

const sources = import.meta.glob<string>(
  [
    "../../**/*.ts",
    "../../**/*.tsx",
    "!../../**/*.test.ts",
    "!../../**/*.test.tsx",
  ],
  { query: "?raw", import: "default", eager: true }
)

/** The functions that put a continuation on the wire, each taking it from
 * its caller. `recordsQueryOptions` is not one: its `FirstPageParams` refuse
 * an `after`, so a cached query never resends a cursor. */
const SENDERS = [
  "listPath",
  "fetchRecordsPage",
  "fetchChangesPage",
  "fetchHistoryPage",
  "watchChanges",
  "followChanges",
  "seekBoundary",
  "seekQueryOptions",
] as const

/** An infinite query's paging: each one walks a cursor with Load more. */
const PAGERS = ["getNextPageParam", "getPreviousPageParam"] as const

type Used = (typeof SENDERS)[number] | (typeof PAGERS)[number]

/** What holds one use to RULE W, or why it needs nothing. */
type By =
  /** A list read whole: its walk starts over under the walk's allowance. */
  | "walkPages"
  /** A read that keeps or resumes a position outside a page walk (the
   * time seek): it starts over under its own allowance. */
  | "startOverWhenRefused"
  /** An infinite query paged with Load more, under its query's allowance;
   * main.tsx installs it on the console's one client. */
  | "restartRefusedWalks"
  /** A watch that registers its reader (`reader:`): reopened at the head
   * after a refusal, under the allowance of the reader's key. */
  | "watchChanges"
  /** A watch that re-reads its surface, through `watchChanges`, its reader
   * registered too. */
  | "followChanges"
  /** A History run's rows, read from the position its summary named:
   * refused once, never sent again, and the summary read again under the
   * feed's allowance (history-feed.tsx, use-history-feed.ts). */
  | "historyRuns"
  /** No continuation at all: a first page alone, or a numbered page by
   * `offset`, which is no position the server handed out. */
  | "firstPage"
  /** A sender's own body, putting its caller's continuation on the wire:
   * held by whatever holds that caller, classified at the caller. */
  | "sender"

interface Use {
  by: By
  why: string
}

const READERS: Record<string, Partial<Record<Used, Use[]>>> = {
  "src/components/changelog/changelog-table.tsx": {
    watchChanges: [{ by: "watchChanges", why: "the History table's tail" }],
  },
  "src/components/record-view/observed-blocks.tsx": {
    fetchRecordsPage: [
      {
        by: "walkPages",
        why: "a chart's summaries, read whole by fetchChartSummaries",
      },
    ],
  },
  "src/hooks/use-applet-runtime.ts": {
    followChanges: [{ by: "followChanges", why: "an applet screen's watch" }],
  },
  "src/hooks/use-history-feed.ts": {
    watchChanges: [{ by: "watchChanges", why: "History's live tail" }],
  },
  "src/hooks/use-live-invalidation.ts": {
    followChanges: [{ by: "followChanges", why: "a page's live re-reads" }],
  },
  "src/hooks/use-live-records.ts": {
    followChanges: [{ by: "followChanges", why: "a collection's live tail" }],
  },
  "src/hooks/use-unit-reader.ts": {
    watchChanges: [{ by: "watchChanges", why: "the unit graph's watch" }],
  },
  "src/lib/api/actors.ts": {
    listPath: [{ by: "walkPages", why: "the actor registry, read whole" }],
  },
  "src/lib/api/agents.ts": {
    fetchRecordsPage: [
      { by: "walkPages", why: "the llm/model records, read whole" },
      { by: "firstPage", why: "readThreadNow: a thread's newest messages" },
    ],
  },
  "src/lib/api/alerts.ts": {
    listPath: [
      { by: "firstPage", why: "the open alerts about some records" },
      { by: "firstPage", why: "every open alert, with the set's count" },
    ],
  },
  "src/lib/api/bundles.ts": {
    listPath: [
      { by: "firstPage", why: "a trait's records, a page said capped" },
      { by: "firstPage", why: "the record mappings, one page" },
    ],
  },
  "src/lib/api/changes.ts": {
    fetchChangesPage: [
      { by: "sender", why: "fetchChangesPage again without `values`" },
      { by: "restartRefusedWalks", why: "the changelog's older pages" },
      { by: "firstPage", why: "the changelog's newest page" },
      {
        by: "startOverWhenRefused",
        why: "the page below a kept time seek, which a refusal drops",
      },
      { by: "sender", why: "fetchHistoryPage's rows from a server sans runs" },
      { by: "historyRuns", why: "a History run's own rows" },
      { by: "firstPage", why: "the time seek's head page" },
      {
        by: "startOverWhenRefused",
        why: "the time seek's probes, under the head page's generation",
      },
    ],
    fetchHistoryPage: [
      { by: "restartRefusedWalks", why: "History's pages of runs" },
    ],
    getNextPageParam: [
      { by: "restartRefusedWalks", why: "the changelog table" },
      { by: "restartRefusedWalks", why: "History" },
    ],
    seekBoundary: [{ by: "startOverWhenRefused", why: "the time seek" }],
    seekQueryOptions: [
      { by: "startOverWhenRefused", why: "the time seek a page reads below" },
      {
        by: "startOverWhenRefused",
        why: "that kept seek, which a refusal drops",
      },
    ],
  },
  "src/lib/api/follow.ts": {
    watchChanges: [
      {
        by: "followChanges",
        why: "each watch it opens, all under its caller's reader",
      },
    ],
  },
  "src/lib/api/functions.ts": {
    fetchRecordsPage: [
      { by: "firstPage", why: "the function records, one page" },
      { by: "firstPage", why: "the newest tool messages" },
      { by: "firstPage", why: "a tool's usage, one row and the count" },
      { by: "firstPage", why: "the turn that dispatched a call" },
      { by: "firstPage", why: "the newest trigger runs" },
    ],
  },
  "src/lib/api/kinds.ts": {
    listPath: [{ by: "walkPages", why: "the kind registry, read whole" }],
  },
  "src/lib/api/records.ts": {
    listPath: [
      { by: "sender", why: "fetchRecordsPage's read" },
      { by: "sender", why: "fetchRecordsPage again without `expand`" },
      { by: "firstPage", why: "the titles of a record's referents" },
      { by: "firstPage", why: "the recordmappings a record names" },
      { by: "firstPage", why: "the merges a record won" },
      { by: "firstPage", why: "the merge requests naming a record" },
      { by: "firstPage", why: "a collection's count, one row" },
      { by: "firstPage", why: "a glance at a collection's size" },
      { by: "firstPage", why: "a count's one-row probes by `offset`" },
      { by: "walkPages", why: "a count's bounded walk" },
      { by: "restartRefusedWalks", why: "what points at a record" },
    ],
    fetchRecordsPage: [
      {
        by: "firstPage",
        why: "recordsQueryOptions, whose FirstPageParams refuse an `after`",
      },
    ],
    fetchChangesPage: [
      { by: "walkPages", why: "a former id's history, read whole" },
      { by: "restartRefusedWalks", why: "a record's history" },
    ],
    getNextPageParam: [
      { by: "restartRefusedWalks", why: "what points at a record" },
      { by: "restartRefusedWalks", why: "a record's history" },
    ],
  },
  "src/lib/api/repository.ts": {
    listPath: [{ by: "firstPage", why: "the repository record" }],
  },
  "src/lib/api/settings.ts": {
    listPath: [{ by: "firstPage", why: "the settings and secrets, one page" }],
  },
  "src/lib/api/sync.ts": {
    listPath: [
      { by: "firstPage", why: "the trigger records, one page" },
      { by: "firstPage", why: "a trigger's newest runs" },
    ],
  },
  "src/lib/identities.ts": {
    fetchRecordsPage: [
      { by: "firstPage", why: "a picker's page, typed FirstPageParams" },
    ],
  },
  "src/lib/packages.ts": {
    fetchRecordsPage: [
      { by: "walkPages", why: "the package records, read whole" },
    ],
  },
  "src/lib/reference-titles.ts": {
    fetchRecordsPage: [{ by: "firstPage", why: "a batch of titles by id" }],
  },
}

/** The functions a walk read whole hands its reads to: `walkPages` itself,
 * and `fetchChartSummaries` (record-view.ts), which reads through
 * `walkPages` (held below). */
const WALKERS = new Set(["walkPages", "fetchChartSummaries"])

/** The query parameters a continuation travels in. */
const CONTINUATION = new Set(["after", "before", "from", "generation"])
const IN_URL = /[?&](after|before|from|generation)=/

/** Writes of one of those names that are no continuation, by file and the
 * function they sit in. */
const OTHER_PARAMETERS: Record<string, string> = {
  "src/lib/api/units.ts convertOnServer":
    "`from` on /units/convert is the unit a value converts from",
}

interface Source {
  /** The file's path under the console (`src/…`). */
  path: string
  source: ts.SourceFile
}

function parse(path: string, text: string): Source {
  return {
    path,
    source: ts.createSourceFile(
      path,
      text,
      ts.ScriptTarget.Latest,
      true,
      path.endsWith(".tsx") ? ts.ScriptKind.TSX : ts.ScriptKind.TS
    ),
  }
}

/** The files, by their path under the console (`src/…`). */
const files = Object.entries(sources).map(([key, text]) =>
  parse(
    new URL(key, "file:///console/src/lib/api/").pathname.slice(
      "/console/".length
    ),
    text
  )
)

/** How a file names what it imports. A binding renamed with `as` is read
 * as the name it imports, so `import { fetchRecordsPage as read }` makes
 * `read(` a use of `fetchRecordsPage`; a namespace import's members are
 * read by their own names (`api.fetchRecordsPage(`). */
interface Imports {
  /** Each named import's local name, and the name it imports. */
  renamed: Map<string, string>
  /** Each namespace import's local name. */
  namespaces: Set<string>
}

function imports(source: ts.SourceFile): Imports {
  const renamed = new Map<string, string>()
  const namespaces = new Set<string>()
  for (const statement of source.statements) {
    const bindings = ts.isImportDeclaration(statement)
      ? statement.importClause?.namedBindings
      : undefined
    if (!bindings) continue
    if (ts.isNamespaceImport(bindings)) namespaces.add(bindings.name.text)
    else
      for (const element of bindings.elements)
        renamed.set(
          element.name.text,
          (element.propertyName ?? element.name).text
        )
  }
  return { renamed, namespaces }
}

/** The name an identifier refers to: its own as a member (`api.x`), else
 * the name it was imported under. */
function resolved(id: ts.Identifier, names: Imports): string {
  const p = id.parent
  if (ts.isPropertyAccessExpression(p) && p.name === id) return id.text
  return names.renamed.get(id.text) ?? id.text
}

/** Whether an identifier names a function it uses, rather than declaring,
 * importing, exporting or typing it. */
function isUse(id: ts.Identifier): boolean {
  const p = id.parent
  if (
    (ts.isFunctionDeclaration(p) ||
      ts.isVariableDeclaration(p) ||
      ts.isParameter(p) ||
      ts.isPropertyAssignment(p) ||
      ts.isMethodDeclaration(p) ||
      ts.isPropertySignature(p)) &&
    p.name === id
  )
    return false
  return !(
    ts.isImportSpecifier(p) ||
    ts.isExportSpecifier(p) ||
    ts.isImportClause(p) ||
    ts.isNamespaceImport(p) ||
    ts.isTypeQueryNode(p)
  )
}

function each(node: ts.Node, visit: (n: ts.Node) => void) {
  visit(node)
  ts.forEachChild(node, (child) => each(child, visit))
}

/** The name of the function a call calls, where it names one. */
function calleeName(
  call: ts.CallExpression,
  names: Imports
): string | undefined {
  const callee = call.expression
  if (ts.isIdentifier(callee)) return resolved(callee, names)
  if (ts.isPropertyAccessExpression(callee)) return callee.name.text
  return undefined
}

/** The call a use is the callee of: `read(…)`, or `api.read(…)`. */
function calledBy(use: ts.Node): ts.CallExpression | undefined {
  if (!ts.isIdentifier(use)) return undefined
  const p = use.parent
  if (ts.isCallExpression(p) && p.expression === use) return p
  if (
    ts.isPropertyAccessExpression(p) &&
    p.name === use &&
    ts.isCallExpression(p.parent) &&
    p.parent.expression === p
  )
    return p.parent
  return undefined
}

/** The name of a property it can read: a word or a string, never a
 * computed one (`[key]`), which could be any. */
function propertyName(name: ts.PropertyName | undefined): string | undefined {
  if (!name) return undefined
  if (ts.isIdentifier(name) || ts.isStringLiteralLike(name)) return name.text
  return undefined
}

/** Whether an object literal can name one of the continuation's
 * parameters: it names one, or computes a property's name. */
function namesContinuation(arg: ts.Expression | undefined): boolean {
  return (
    arg !== undefined &&
    ts.isObjectLiteralExpression(arg) &&
    arg.properties.some((prop) => {
      if (prop.name && ts.isComputedPropertyName(prop.name)) return true
      const name = propertyName(prop.name)
      return name !== undefined && CONTINUATION.has(name)
    })
  )
}

/** Whether a call registers its reader: an object literal naming
 * `reader`, which the type requires and this holds where it sits. */
function registersReader(call: ts.CallExpression): boolean {
  const arg = call.arguments[0]
  return (
    arg !== undefined &&
    ts.isObjectLiteralExpression(arg) &&
    arg.properties.some((prop) => propertyName(prop.name) === "reader")
  )
}

/** The classifications a use's place in the source fits. A first page
 * names no continuation in what it passes (what is passed whole is typed
 * `FirstPageParams` where it reaches a list). */
function fits(use: ts.Node, names: Imports): Set<By> {
  const out = new Set<By>()
  const call = calledBy(use)
  const callee = call && calleeName(call, names)
  if (call && !namesContinuation(call.arguments[0])) out.add("firstPage")
  if (call && callee === "followChanges" && registersReader(call))
    out.add("followChanges")
  if (call && callee === "watchChanges" && registersReader(call))
    out.add("watchChanges")
  const senders = new Set<string>(SENDERS)
  for (let p = use.parent; p as ts.Node | undefined; p = p.parent) {
    if (ts.isCallExpression(p) && p !== call) {
      const name = calleeName(p, names) ?? ""
      if (WALKERS.has(name)) out.add("walkPages")
      if (name === "startOverWhenRefused") out.add("startOverWhenRefused")
      if (name === "infiniteQueryOptions") out.add("restartRefusedWalks")
    }
    if (ts.isFunctionDeclaration(p) && p.name) {
      if (p.name.text === "runRowsQueryOptions") out.add("historyRuns")
      if (p.name.text === "followChanges") out.add("followChanges")
      if (senders.has(p.name.text)) out.add("sender")
    }
  }
  return out
}

/** Every use of a sender and every pager, by file and name, each with the
 * classifications its place fits. */
function found(
  scanned: readonly Source[]
): Record<string, Record<string, Set<By>[]>> {
  const senders = new Set<string>(SENDERS)
  const pagers = new Set<string>(PAGERS)
  const out: Record<string, Record<string, Set<By>[]>> = {}
  const add = (path: string, name: string, use: Set<By>) => {
    out[path] ??= {}
    ;(out[path][name] ??= []).push(use)
  }
  for (const { path, source } of scanned) {
    const names = imports(source)
    each(source, (n) => {
      if (ts.isIdentifier(n) && isUse(n)) {
        const name = resolved(n, names)
        if (senders.has(name)) add(path, name, fits(n, names))
      }
      if (
        (ts.isPropertyAssignment(n) ||
          ts.isMethodDeclaration(n) ||
          ts.isShorthandPropertyAssignment(n)) &&
        ts.isIdentifier(n.name) &&
        pagers.has(n.name.text)
      )
        add(path, n.name.text, fits(n, names))
    })
  }
  return out
}

/** The places a sender is reached by a name the scan cannot read: a member
 * picked by a computed name (`api["fetchRecordsPage"](`, `api[name](`) and
 * a sender exported again under another name. Each is refused. */
function unreadable(scanned: readonly Source[]): string[] {
  const senders = new Set<string>(SENDERS)
  const out: string[] = []
  for (const { path, source } of scanned) {
    const names = imports(source)
    const at = (n: ts.Node) =>
      `${path}:${source.getLineAndCharacterOfPosition(n.getStart()).line + 1}`
    each(source, (n) => {
      if (ts.isElementAccessExpression(n)) {
        const arg = n.argumentExpression
        const member = ts.isStringLiteralLike(arg) ? arg.text : undefined
        const onNamespace =
          ts.isIdentifier(n.expression) &&
          names.namespaces.has(n.expression.text)
        if (
          (member !== undefined && senders.has(member)) ||
          (onNamespace && member === undefined)
        )
          out.push(`${at(n)} reaches a sender by a computed name`)
      }
      if (
        ts.isExportSpecifier(n) &&
        n.propertyName &&
        senders.has(n.propertyName.text) &&
        n.propertyName.text !== n.name.text
      )
        out.push(`${at(n)} exports ${n.propertyName.text} as ${n.name.text}`)
    })
  }
  return out
}

/** Whether every classification in `listed` can be given to its own use: a
 * matching of the table's entries onto the uses whose place fits them. */
function matches(listed: readonly By[], uses: readonly Set<By>[]): boolean {
  const taken = new Array<number | undefined>(uses.length)
  const place = (entry: number, seen: Set<number>): boolean => {
    for (let u = 0; u < uses.length; u++) {
      if (seen.has(u) || !uses[u].has(listed[entry])) continue
      seen.add(u)
      const held = taken[u]
      if (held === undefined || place(held, seen)) {
        taken[u] = entry
        return true
      }
    }
    return false
  }
  return listed.every((_, entry) => place(entry, new Set()))
}

/** The function declaration a node sits in, by name. */
function enclosingFunction(node: ts.Node): string | undefined {
  for (let p = node.parent; p as ts.Node | undefined; p = p.parent)
    if (ts.isFunctionDeclaration(p)) return p.name?.text
  return undefined
}

/** The functions, in any file, declared to answer `URLSearchParams`
 * (`changesSearch`): what one answers is search parameters. */
function searchParamsMakers(scanned: readonly Source[]): Set<string> {
  const out = new Set<string>()
  for (const { source } of scanned)
    each(source, (n) => {
      if (
        ts.isFunctionDeclaration(n) &&
        n.name &&
        n.type &&
        n.type.getText(source) === "URLSearchParams"
      )
        out.add(n.name.text)
    })
  return out
}

/** Whether an expression is search parameters: `new URLSearchParams(…)`, a
 * URL's `.searchParams`, a maker's answer, or a name bound to one of
 * those. */
function isSearchParams(
  e: ts.Expression,
  bound: ReadonlySet<string>,
  makers: ReadonlySet<string>,
  source: ts.SourceFile
): boolean {
  if (ts.isNewExpression(e))
    return (
      ts.isIdentifier(e.expression) && e.expression.text === "URLSearchParams"
    )
  if (ts.isPropertyAccessExpression(e)) return e.name.text === "searchParams"
  if (ts.isCallExpression(e))
    return ts.isIdentifier(e.expression) && makers.has(e.expression.text)
  if (ts.isIdentifier(e)) return bound.has(e.text)
  if (ts.isParenthesizedExpression(e))
    return isSearchParams(e.expression, bound, makers, source)
  return false
}

/** Where the source writes a continuation's parameter, or a parameter whose
 * name it computes, which could be one: a `set` or `append` on search
 * parameters, a `URLSearchParams` built from an object, or a URL spelled
 * with one. */
function parameterWrites(
  scanned: readonly Source[]
): { path: string; line: number; inside?: string }[] {
  const makers = searchParamsMakers(scanned)
  const out: { path: string; line: number; inside?: string }[] = []
  for (const { path, source } of scanned) {
    const bound = new Set<string>()
    each(source, (n) => {
      if (
        ts.isVariableDeclaration(n) &&
        ts.isIdentifier(n.name) &&
        ((n.initializer &&
          isSearchParams(n.initializer, bound, makers, source)) ||
          n.type?.getText(source) === "URLSearchParams")
      )
        bound.add(n.name.text)
      if (
        ts.isParameter(n) &&
        ts.isIdentifier(n.name) &&
        n.type?.getText(source) === "URLSearchParams"
      )
        bound.add(n.name.text)
    })
    each(source, (n) => {
      let writes = false
      if (
        ts.isCallExpression(n) &&
        ts.isPropertyAccessExpression(n.expression) &&
        ["set", "append"].includes(n.expression.name.text) &&
        n.arguments.length === 2
      ) {
        const name = n.arguments[0]
        if (ts.isStringLiteralLike(name)) writes = CONTINUATION.has(name.text)
        else
          writes = isSearchParams(
            n.expression.expression,
            bound,
            makers,
            source
          )
      }
      if (
        ts.isNewExpression(n) &&
        ts.isIdentifier(n.expression) &&
        n.expression.text === "URLSearchParams" &&
        namesContinuation(n.arguments?.[0])
      )
        writes = true
      if (
        (ts.isStringLiteralLike(n) ||
          ts.isTemplateHead(n) ||
          ts.isTemplateMiddle(n) ||
          ts.isTemplateTail(n)) &&
        IN_URL.test(n.text)
      )
        writes = true
      if (writes)
        out.push({
          path,
          line: source.getLineAndCharacterOfPosition(n.getStart()).line + 1,
          inside: enclosingFunction(n),
        })
    })
  }
  return out
}

/** The writes outside a sender that nothing names another job for. */
function strayWrites(scanned: readonly Source[]): string[] {
  const senders = new Set<string>(SENDERS)
  return parameterWrites(scanned)
    .filter((w) => !w.inside || !senders.has(w.inside))
    .filter((w) => !(`${w.path} ${w.inside}` in OTHER_PARAMETERS))
    .map((w) => `${w.path}:${w.line}`)
}

describe("every place that sends a continuation", () => {
  const counted = found(files)

  it("reads the console's source", () => {
    expect(files.length).toBeGreaterThan(100)
    expect(files.some((f) => f.path === "src/lib/api/compacted.ts")).toBe(true)
    expect(files.some((f) => f.path.endsWith(".test.ts"))).toBe(false)
  })

  it("is in the table, in every file, with every use classified", () => {
    const moved: string[] = []
    for (const [path, names] of Object.entries(counted))
      for (const [name, uses] of Object.entries(names)) {
        const listed = READERS[path]?.[name as Used]
        if (!listed)
          moved.push(`${path}: ${name} used ${uses.length}×, not in READERS`)
        else if (listed.length !== uses.length)
          moved.push(
            `${path}: ${name} used ${uses.length}×, READERS classifies ${listed.length}`
          )
      }
    expect(moved).toEqual([])
  })

  it("classifies each use by what holds it where it sits", () => {
    const misplaced: string[] = []
    for (const [path, names] of Object.entries(READERS))
      for (const [name, listed] of Object.entries(names)) {
        const uses = counted[path]?.[name]
        if (!uses || uses.length !== listed.length) continue
        if (
          !matches(
            listed.map((u) => u.by),
            uses
          )
        )
          misplaced.push(
            `${path}: ${name} is classified ${listed.map((u) => u.by).join(", ")}; its uses fit ${uses.map((u) => `[${[...u].join(" ")}]`).join(", ")}`
          )
      }
    expect(misplaced).toEqual([])
  })

  it("reads a chart's summaries through walkPages", () => {
    const chart = files.find((f) => f.path === "src/lib/record-view.ts")
    let walks = false
    if (chart)
      each(chart.source, (n) => {
        if (
          ts.isCallExpression(n) &&
          calleeName(n, imports(chart.source)) === "walkPages" &&
          enclosingFunction(n) === "fetchChartSummaries"
        )
          walks = true
      })
    expect(walks).toBe(true)
  })

  it("lists nothing the source no longer holds", () => {
    const gone: string[] = []
    for (const [path, names] of Object.entries(READERS))
      for (const name of Object.keys(names))
        if (!counted[path]?.[name]) gone.push(`${path}: ${name}`)
    expect(gone).toEqual([])
  })

  it("leaves the sender's own body to the senders' files", () => {
    const own = new Set(["src/lib/api/records.ts", "src/lib/api/changes.ts"])
    const strays = Object.entries(READERS).flatMap(([path, names]) =>
      Object.values(names).some((uses) => uses.some((u) => u.by === "sender"))
        ? own.has(path)
          ? []
          : [path]
        : []
    )
    expect(strays).toEqual([])
  })

  it("writes a continuation's parameter only inside a sender", () => {
    const writes = parameterWrites(files)
    expect(writes.length).toBeGreaterThan(0)
    expect(strayWrites(files)).toEqual([])
    const other = new Set(writes.map((w) => `${w.path} ${w.inside}`))
    expect(
      Object.keys(OTHER_PARAMETERS).filter((key) => !other.has(key))
    ).toEqual([])
  })

  it("reaches every sender by a name it can read", () => {
    expect(unreadable(files)).toEqual([])
  })
})

/** The scan itself, over sources written to slip past it. */
describe("the scan", () => {
  const fixture = (text: string) => [parse("src/fixture.ts", text)]
  const uses = (text: string) => found(fixture(text))["src/fixture.ts"] ?? {}

  it("reads a sender imported under another name as that sender", () => {
    const read = uses(`
      import { fetchRecordsPage as read } from "./records"
      export function older(cursor: string) {
        return read({ authority: "a", package: "p", name: "n", after: cursor })
      }
      export function newest() {
        return read({ authority: "a", package: "p", name: "n" })
      }
    `)
    expect(Object.keys(read)).toEqual(["fetchRecordsPage"])
    expect(read.fetchRecordsPage.map((fit) => fit.has("firstPage"))).toEqual([
      false,
      true,
    ])
  })

  it("reads a namespace import's member as the sender it names", () => {
    const read = uses(`
      import * as api from "./changes"
      export function older(before: number, generation: string) {
        return api.fetchChangesPage({ before, generation })
      }
      export function follow(key: string) {
        return api.watchChanges({
          reader: { key, drop: () => {}, fail: () => {} },
          onRow: () => {},
          onCompacted: () => {},
        })
      }
    `)
    expect(read.fetchChangesPage).toHaveLength(1)
    expect(read.fetchChangesPage[0].has("firstPage")).toBe(false)
    expect(read.watchChanges).toHaveLength(1)
    expect(read.watchChanges[0].has("watchChanges")).toBe(true)
  })

  it("holds a watch that registers no reader to nothing", () => {
    const read = uses(`
      import { watchChanges as watch } from "./changes"
      export function bare(opts: Parameters<typeof watch>[0]) {
        return watch(opts)
      }
    `)
    expect(read.watchChanges[0].has("watchChanges")).toBe(false)
  })

  it("refuses a sender reached by a computed name or exported under another", () => {
    const refused = unreadable(
      fixture(`
        import * as api from "./records"
        export { listPath as path } from "./records"
        export const read = () => api["fetchRecordsPage"]({ name: "n" })
        export const pick = (name: "listPath") => api[name]
      `)
    )
    expect(refused).toEqual([
      "src/fixture.ts:3 exports listPath as path",
      "src/fixture.ts:4 reaches a sender by a computed name",
      "src/fixture.ts:5 reaches a sender by a computed name",
    ])
  })

  it("counts a parameter whose name is computed as a continuation", () => {
    const text = `
      import { fetchRecordsPage } from "./records"
      const key = "after"
      export function older(cursor: string) {
        return fetchRecordsPage({ name: "n", [key]: cursor })
      }
      export function query(name: string, cursor: string) {
        const params = new URLSearchParams()
        params.set(name, cursor)
        const url = new URL("http://x")
        url.searchParams.append(name, cursor)
        return new URLSearchParams({ [key]: cursor, "before": cursor })
      }
      export function kept(name: string) {
        const seen = new Map<string, string>()
        seen.set(name, "a map, not a query")
      }
    `
    expect(uses(text).fetchRecordsPage[0].has("firstPage")).toBe(false)
    expect(strayWrites(fixture(text))).toEqual([
      "src/fixture.ts:9",
      "src/fixture.ts:11",
      "src/fixture.ts:12",
    ])
  })
})
