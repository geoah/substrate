import { Fragment, useRef, type PointerEvent, type ReactNode } from "react"

import type { SubstrateRecord } from "@/lib/api/types"
import {
  INDEX_SECTIONS,
  groupRecords,
  type PropertyHint,
  type RecordGroup,
} from "./buckets"
import { Empty } from "./empty"
import { useNow } from "./now"
import { useKind, type Page } from "./sdk"
import { Spinner } from "./spinner"

export type ListPage = Page & { loadMore?(): void }

export interface ListProps<T extends SubstrateRecord> {
  /** The page a `useRecords` returns; `records` is the alternative for rows
   * the app already holds (a local split of one page). */
  page?: ListPage
  records?: readonly T[]
  /** A property name (a datetime buckets Overdue to Undated, a state or an
   * enum sections by value) or a function of the row. */
  groupBy?: string | ((record: T) => string)
  /** An A–Z rail down the right edge; the sections are `groupBy`'s keys. */
  index?: boolean
  /** What an empty list says: a title, or a node drawn in its place. */
  empty?: ReactNode
  children: (record: T) => ReactNode
}

const NO_RECORDS: readonly never[] = []

/** The kit's list: rows from a page or an array, sectioned, with the page's
 * own footer (Load more while a cursor remains, a note when the page was
 * cut). The declaration of the first row's kind, when the SDK holds it,
 * says how a named property sections; without it the values decide. */
export function List<T extends SubstrateRecord>({
  page,
  records,
  groupBy,
  index,
  empty,
  children,
}: ListProps<T>) {
  const rows = (page?.records as T[] | undefined) ?? records ?? NO_RECORDS
  const declaration = useKind(rows[0]?.kind ?? "")
  const hint: PropertyHint | undefined =
    typeof groupBy === "string"
      ? declaration?.properties.find(
          (p: { name: string }) => p.name === groupBy
        )
      : undefined
  const sections = useRef(new Map<string, HTMLElement>())
  const now = useNow()

  if (!rows.length) {
    if (page?.loading) {
      return (
        <div className="kit-loading">
          <Spinner />
        </div>
      )
    }
    if (page?.error) {
      return <Empty title="Could not load" description={page.error.message} />
    }
    if (typeof empty === "string" || empty === undefined) {
      return <Empty title={empty ?? "Nothing here"} />
    }
    return <>{empty}</>
  }

  const groups: RecordGroup<T>[] = groupBy
    ? groupRecords(rows, groupBy, hint, now)
    : [{ key: "", label: "", records: [...rows] }]
  const rail = Boolean(index && groupBy)
  const jump = (key: string) =>
    sections.current.get(key)?.scrollIntoView({ block: "start" })
  const onRailPointer = (e: PointerEvent<HTMLDivElement>) => {
    const el = document.elementFromPoint(e.clientX, e.clientY)
    const key = el instanceof HTMLElement ? el.dataset.section : undefined
    if (key && sections.current.has(key)) jump(key)
  }

  return (
    <div className={rail ? "kit-list kit-list--index" : "kit-list"}>
      {groups.map((group) => (
        <section
          key={group.key}
          className="kit-list-group"
          aria-label={group.label || undefined}
          ref={(el) => {
            if (el) sections.current.set(group.key, el)
            else sections.current.delete(group.key)
          }}
        >
          {group.label && (
            <div className="kit-list-header">
              <span>{group.label}</span>
              <span className="kit-list-count">{group.records.length}</span>
            </div>
          )}
          <div role="list">
            {group.records.map((record) => (
              <Fragment key={record.id}>{children(record)}</Fragment>
            ))}
          </div>
        </section>
      ))}
      {page?.cursor && page.loadMore && (
        <button
          type="button"
          className="kit-btn kit-list-more"
          disabled={page.loading}
          onClick={() => page.loadMore?.()}
        >
          {page.loading ? "Loading…" : "Load more"}
        </button>
      )}
      {page?.incomplete && (
        <p className="kit-list-foot">
          Showing the first {rows.length}; more were not loaded.
        </p>
      )}
      {rail && (
        <div className="kit-index">
          <div
            className="kit-index-rail"
            role="navigation"
            aria-label="Index"
            onPointerDown={onRailPointer}
            onPointerMove={(e) => e.buttons > 0 && onRailPointer(e)}
          >
            {railKeys(groups).map((key) => {
              const present = groups.some((g) => g.key === key)
              return (
                <span
                  key={key}
                  data-section={present ? key : undefined}
                  className={
                    present
                      ? "kit-index-item"
                      : "kit-index-item kit-index-item--absent"
                  }
                  aria-hidden={!present}
                >
                  {key}
                </span>
              )
            })}
          </div>
        </div>
      )}
    </div>
  )
}

/** The rail's letters: the full A–Z when the sections are index keys, with
 * the missing ones dimmed, else the sections themselves. */
function railKeys(groups: readonly RecordGroup[]): string[] {
  const keys = groups.map((g) => g.key)
  if (!keys.every((k) => k.length === 1)) return keys
  const extra = keys.filter((k) => !INDEX_SECTIONS.includes(k))
  return [...INDEX_SECTIONS, ...extra]
}
