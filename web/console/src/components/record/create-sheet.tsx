/** A new record is the record page with nothing saved yet: the title as a
 * large input at the top, the same property sheet with the same editors, and
 * the body under a divider. Rows read as a new record is filled in: what it
 * must have, then what it is commonly given (the records it points at, the
 * times it is about), and the rest folded.
 *
 * The sheet edits a DRAFT (`SheetDraftContext`), and the draft is the same
 * YAML document the technical lens edits: a write lands on one key of it, so
 * the two lenses stay one document and switching loses nothing.
 *
 * Nothing is named wrong before the person has said anything: a row's problem
 * shows once its value moved from where it started, or once Create is asked
 * for. */

import { useMemo, useState, type ReactNode } from "react"
import { AlertTriangleIcon } from "lucide-react"

import {
  SheetDraftContext,
  type SheetDraft,
} from "@/components/property-sheet/draft"
import { PageHeader } from "@/components/identity/page-header"
import { pageTitleClass } from "@/components/identity/page-title"
import { PropertySheet } from "@/components/property-sheet/property-sheet"
import type { SheetRow } from "@/components/property-sheet/sheet-rows"
import { RecordBody } from "@/components/record/record-body"
import { useTechnicalDetails } from "@/hooks/use-console-preferences"
import { grantHints, AGENT_KIND } from "@/lib/agent-grants"
import type { KindInfo, SubstrateRecord } from "@/lib/api/types"
import {
  AUTHORITY_PROPERTY,
  PACKAGE_PROPERTY,
  authorityIsDerived,
  declarationIdShape,
  derivedAuthority,
  derivedPackage,
  isDeclarationKind,
  packageIsDerived,
} from "@/lib/declarations"
import { displayName, lowerFirst, untitled } from "@/lib/kind-names"
import { arrangeNewRecord, rowProblem } from "@/lib/record-form"
import {
  bodyProperty,
  propSpecsByName,
  systemSpecs,
  titleEditor,
} from "@/lib/record-schema"
import {
  deleteIn,
  parseApplyDoc,
  propertiesOf,
  setIn,
  type Problem,
} from "@/lib/record-yaml"
import { cn } from "@/lib/utils"

const same = (a: unknown, b: unknown) =>
  JSON.stringify(a ?? null) === JSON.stringify(b ?? null)

/** The one error line under a row or the heading, in the sheet's style. */
function RowError({ children }: { children: ReactNode }) {
  return (
    <p
      role="alert"
      className="mt-1 rounded-md bg-bad-soft px-2.5 py-1.5 text-[12.5px] text-destructive"
    >
      {children}
    </p>
  )
}

export function CreateSheet({
  text,
  kind,
  kinds,
  onChange,
  meta,
  glyph,
  actions,
  seed,
  problems = [],
  attempted = false,
}: {
  text: string
  /** The document the new record started from; absent, the first text this
   * sheet was handed. A row has been changed when it moved from here. */
  seed?: string
  kind: KindInfo
  kinds: KindInfo[]
  onChange: (text: string) => void
  /** One quiet line under the title. */
  meta?: ReactNode
  /** The head's glyph and actions, on the row above the title. */
  glyph?: ReactNode
  actions?: ReactNode
  /** The document's problems, as the create would meet them. */
  problems?: Problem[]
  /** Create was asked for: every problem is named, touched or not. */
  attempted?: boolean
}) {
  const [technical] = useTechnicalDetails()
  const properties = propertiesOf(text)
  const [started] = useState(() => propertiesOf(seed ?? text) ?? {})
  const declared = isDeclarationKind(kind.identity)
  const derivesAuthority = authorityIsDerived(kind.identity)
  const derivesPackage = packageIsDerived(kind.identity)
  const titleSpec = titleEditor(kind)
  const body = bodyProperty(kind)
  const noun = lowerFirst(displayName(kind))
  const id = idOf(text)

  /** Whether a property moved from where the new record started. */
  const changed = (name: string) => !same(properties?.[name], started[name])
  /** Whether a property has been said anything about: it changed, or Create
   * was asked for. */
  const asked = (name: string) => attempted || changed(name)
  const specs = useMemo(
    () =>
      new Map(
        [...systemSpecs(kind), ...propSpecsByName(kind)].map((s) => [s.name, s])
      ),
    [kind]
  )
  const errors: Record<string, string> = {}
  let idError: string | undefined
  for (const problem of problems) {
    if (problem.severity !== "error" || !problem.path) continue
    if (problem.path === "metadata.id") {
      if (attempted || id) idError ??= problem.message.replace(/`/g, "")
      continue
    }
    const spec = specs.get(problem.path)
    if (!spec || !asked(spec.name) || errors[spec.name]) continue
    errors[spec.name] = rowProblem(problem, spec)
  }

  const draft: SheetDraft = {
    write(props) {
      let doc = text
      for (const [name, value] of Object.entries(props)) {
        const path = ["data", "properties", name]
        doc = value === null ? deleteIn(doc, path) : setIn(doc, path, value)
      }
      onChange(doc)
    },
    arrange(rows) {
      const offered = rows.flatMap((row): SheetRow[] => {
        if (row.spec.kind === "state") {
          // A new record starts where the machine starts it; a move is a
          // transition, and a transition needs a record to move.
          return [
            {
              ...row,
              field: undefined,
              hint: `Set by moving it after the ${noun} exists`,
            },
          ]
        }
        if (!row.field) return []
        if (derivesAuthority && row.name === AUTHORITY_PROPERTY) return []
        if (derivesPackage && row.name === PACKAGE_PROPERTY) return []
        return [row]
      })
      return arrangeNewRecord(
        kind,
        offered,
        (row) => Boolean(errors[row.name]) || changed(row.name)
      )
    },
    errors,
  }

  if (properties === undefined) {
    return (
      <p className="py-4 text-sm text-muted-foreground">
        This YAML doesn’t read as a record yet, so the properties can’t be
        shown. Fix it in the YAML and they come back.
      </p>
    )
  }

  const record: SubstrateRecord = {
    id,
    kind: kind.identity,
    properties,
    labels: {},
    version: 0,
    createdAt: "",
    updatedAt: "",
  }

  function setRecordId(next: string) {
    let doc = setIn(text, ["metadata", "id"], next)
    const authority = derivedAuthority(kind.identity, next)
    if (derivesAuthority && authority !== undefined) {
      doc = setIn(doc, ["data", "properties", AUTHORITY_PROPERTY], authority)
    }
    const pkg = derivedPackage(kind.identity, next)
    if (derivesPackage && pkg !== undefined) {
      doc = setIn(doc, ["data", "properties", PACKAGE_PROPERTY], pkg)
    }
    onChange(doc)
  }

  function setTitle(value: string) {
    if (!titleSpec) return
    const path = ["data", "properties", titleSpec.name]
    onChange(value ? setIn(text, path, value) : deleteIn(text, path))
  }

  const titleValue = titleSpec ? String(properties[titleSpec.name] ?? "") : ""
  const hints = kind.identity === AGENT_KIND ? grantHints(properties) : []
  const elsewhere = Object.keys(properties).filter((name) => !specs.has(name))
  const showId = declared || technical

  return (
    <SheetDraftContext.Provider value={draft}>
      <div data-slot="create-sheet">
        <PageHeader
          size="record"
          glyph={glyph}
          actions={actions}
          heading={
            <>
              {titleSpec ? (
                <input
                  aria-label={titleSpec.label}
                  placeholder={untitled(kind)}
                  value={titleValue}
                  autoFocus
                  onChange={(e) => setTitle(e.target.value)}
                  className={cn(
                    pageTitleClass("record"),
                    "mt-2.5 w-full border-0 bg-transparent outline-none placeholder:text-faint"
                  )}
                />
              ) : (
                <h1
                  className={cn(pageTitleClass("record"), "mt-2.5 text-faint")}
                >
                  {untitled(kind)}
                </h1>
              )}
              {titleSpec && errors[titleSpec.name] && (
                <RowError>{errors[titleSpec.name]}</RowError>
              )}
            </>
          }
          meta={meta}
        />

        {hints.length > 0 && (
          <ul className="mt-3 flex flex-col gap-1.5 rounded-md border border-warning/40 bg-warn-soft p-3">
            {hints.map((hint) => (
              <li
                key={hint.function}
                className="flex items-start gap-2 text-xs text-muted-foreground"
              >
                <AlertTriangleIcon className="mt-0.5 size-3.5 shrink-0 text-warning" />
                <span>{hint.message}</span>
              </li>
            ))}
          </ul>
        )}

        {showId && (
          <div className="mt-[18px] grid grid-cols-[minmax(96px,120px)_minmax(0,1fr)] sm:grid-cols-[minmax(120px,170px)_minmax(0,1fr)]">
            <label
              htmlFor="new-record-id"
              className="flex min-h-9 items-center pl-0.5 text-[13.5px] text-muted-foreground"
            >
              ID
            </label>
            <div className="flex min-h-9 min-w-0 flex-col justify-center px-2">
              <input
                id="new-record-id"
                value={id}
                aria-invalid={Boolean(idError)}
                placeholder={
                  declared
                    ? declarationIdShape(kind.identity)
                    : "Leave empty and one is made for you"
                }
                onChange={(e) => setRecordId(e.target.value)}
                className="h-8 w-full rounded-md border border-input bg-transparent px-2 font-mono text-[13px] outline-none placeholder:font-sans focus-visible:border-primary focus-visible:ring-3 focus-visible:ring-primary-soft"
              />
            </div>
            {idError && (
              <div className="col-start-2 px-2">
                <RowError>{idError}</RowError>
              </div>
            )}
          </div>
        )}

        <PropertySheet record={record} kind={kind} kinds={kinds} />

        {body && <RecordBody record={record} spec={body} readOnly={false} />}
        {body && errors[body.name] && <RowError>{errors[body.name]}</RowError>}

        {technical && elsewhere.length > 0 && (
          <p className="mt-3 text-xs text-muted-foreground">
            <span className="font-mono">{elsewhere.join(", ")}</span>{" "}
            {elsewhere.length === 1 ? "is" : "are"} in the YAML but not
            declared, so {elsewhere.length === 1 ? "it isn’t" : "they aren’t"}{" "}
            shown here. {elsewhere.length === 1 ? "It is" : "They are"} saved as
            written.
          </p>
        )}
      </div>
    </SheetDraftContext.Provider>
  )
}

function idOf(text: string): string {
  const id = parseApplyDoc(text).value?.metadata?.id
  return typeof id === "string" ? id : ""
}
