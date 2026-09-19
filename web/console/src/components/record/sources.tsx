/** Sources: the records that map ONTO this one, grouped by the mapping that
 * brought each of them, and the records merged away into it.
 *
 * A person that eight provider rows converged on used to read as eight
 * unlabelled pills (`<title> user beeperuserperson`, eight times) under the
 * properties, and again as an expandable sub-graph per row on the Graph tab.
 * This is the one place they are now said, and the mapping is the unit: its
 * header is the mapping's title as a link to the declaration, the kind it
 * reads as a link to that collection, the count, and "contributes: name,
 * emails, phones" off its `map` rules — so a reader learns WHY the eight rows
 * are here before reading one. The members are the standard RecordPill,
 * deduplicated by record, sorted by title, ten at a time.
 *
 * Merged-away records are the other kind of source: their sources now point
 * here by resolution. They sit under "Merged", each with the `recordmerge`
 * that joined them and the request that proposed it, where one did. */

import { useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import { CombineIcon, GitMergeIcon } from "lucide-react"

import { RecordPill } from "@/components/record-pill"
import { Button } from "@/components/ui/button"
import { CORE_PACKAGE, splitKind } from "@/lib/api/http"
import {
  mergeRequestsForQueryOptions,
  mergesIntoQueryOptions,
  recordPath,
} from "@/lib/api/records"
import {
  readReference,
  type KindInfo,
  type SubstrateRecord,
} from "@/lib/api/types"
import { kindByIdentity } from "@/lib/definition"
import { recordTitle } from "@/lib/format"
import { groupSources, type SourceGroup } from "@/lib/provenance"
import { splitRecordPath } from "@/lib/record-path"

/** How many members a group shows before "show all". */
const FOLD_AT = 10

/** A kind reference as a link to its collection, inert when the registry does
 * not know it. */
function KindLink({ kind, kinds }: { kind: string; kinds: KindInfo[] }) {
  const info = kindByIdentity(kinds, kind)
  if (!info) {
    return <span className="data break-all">{kind}</span>
  }
  return (
    <Link
      to="/data/$authority/$pkg/$name"
      params={{ authority: info.authority, pkg: info.package, name: info.name }}
      className="data break-all text-primary underline-offset-4 hover:underline"
    >
      {kind}
    </Link>
  )
}

function Group({ group, kinds }: { group: SourceGroup; kinds: KindInfo[] }) {
  const [all, setAll] = useState(false)
  const shown = all ? group.members : group.members.slice(0, FOLD_AT)
  const mappingKind = `${CORE_PACKAGE}/recordmapping`
  const mappingRoutable = Boolean(kindByIdentity(kinds, mappingKind))
  return (
    <section className="min-w-0 rounded-xl border bg-card p-4">
      <header className="flex min-w-0 flex-col gap-1.5">
        <div className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1">
          <h3 className="text-sm font-semibold">
            <RecordPill
              kind={mappingRoutable ? mappingKind : ""}
              id={group.mapping}
              title={group.title}
            />
          </h3>
          <span className="text-xs text-muted-foreground">
            {group.members.length.toLocaleString()}{" "}
            {group.members.length === 1 ? "record" : "records"}
          </span>
          {!group.declared && (
            <span className="text-xs text-destructive">
              mapping declaration not found
            </span>
          )}
        </div>
        {group.description && (
          <p className="text-xs text-muted-foreground">{group.description}</p>
        )}
        <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-1 text-xs">
          <dt className="text-muted-foreground">reads</dt>
          <dd className="min-w-0">
            <KindLink kind={group.from} kinds={kinds} />
            <span className="text-muted-foreground">
              {" "}
              through its <code>{group.property}</code> slot
            </span>
          </dd>
          <dt className="text-muted-foreground">contributes</dt>
          <dd className="min-w-0">
            {group.contributes.length ? (
              <span className="data break-words">
                {group.contributes.join(", ")}
              </span>
            ) : (
              <span className="text-muted-foreground">
                nothing: a link-only mapping
              </span>
            )}
          </dd>
        </dl>
      </header>
      <ul className="mt-3 flex flex-wrap gap-1.5">
        {shown.map((m) => (
          <li key={m.ref} className="min-w-0">
            <RecordPill
              kind={kindByIdentity(kinds, m.kind) ? m.kind : ""}
              id={m.id}
              title={m.title}
            />
          </li>
        ))}
      </ul>
      {group.members.length > FOLD_AT && (
        <Button
          variant="ghost"
          size="sm"
          className="mt-2 h-6 px-1 text-xs font-normal text-muted-foreground"
          onClick={() => setAll((v) => !v)}
        >
          {all
            ? "Show fewer"
            : `Show all ${group.members.length.toLocaleString()}`}
        </Button>
      )}
    </section>
  )
}

/** One merged-away record: the loser, the merge that joined it, and the
 * request that proposed the merge where one did. */
function MergedRow({
  merge,
  request,
  kind,
  kinds,
}: {
  merge: SubstrateRecord
  request?: SubstrateRecord
  kind: string
  kinds: KindInfo[]
}) {
  const loser = splitRecordPath(
    readReference(merge.properties.loser)?.path ?? ""
  )
  const mergeKind = `${CORE_PACKAGE}/recordmerge`
  const requestKind = `${CORE_PACKAGE}/recordmergerequest`
  return (
    <li className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1">
      <RecordPill
        kind={kindByIdentity(kinds, kind) ? kind : ""}
        id={loser?.id ?? merge.id}
      />
      <span className="text-xs text-muted-foreground">
        merged in by{" "}
        <RecordPill
          kind={kindByIdentity(kinds, mergeKind) ? mergeKind : ""}
          id={merge.id}
          title={recordTitle(merge.properties) || undefined}
        />
      </span>
      {request && (
        <span className="text-xs text-muted-foreground">
          from request{" "}
          <RecordPill
            kind={kindByIdentity(kinds, requestKind) ? requestKind : ""}
            id={request.id}
            title={recordTitle(request.properties) || undefined}
          />
        </span>
      )}
    </li>
  )
}

function Merged({
  record,
  kinds,
}: {
  record: SubstrateRecord
  kinds: KindInfo[]
}) {
  const former = record.formerIds ?? []
  const path = recordPath(record.kind, record.id)
  const merges = useQuery(mergesIntoQueryOptions(path, former.length > 0))
  const requests = useQuery(
    mergeRequestsForQueryOptions(path, former.length > 0)
  )
  if (!former.length) return null
  const rows = merges.data?.records ?? []
  const requestFor = (loserPath: string | undefined) =>
    (requests.data?.records ?? []).find(
      (r) => readReference(r.properties.loser)?.path === loserPath
    )
  return (
    <section className="min-w-0 rounded-xl border bg-card p-4">
      <header className="flex items-center gap-2">
        <GitMergeIcon className="size-3.5 text-muted-foreground" />
        <h3 className="text-sm font-semibold">Merged</h3>
        <span className="text-xs text-muted-foreground">
          {former.length.toLocaleString()}{" "}
          {former.length === 1 ? "record" : "records"} merged into this one;
          their sources now resolve here
        </span>
      </header>
      <ul className="mt-3 flex flex-col gap-1.5">
        {rows.map((merge) => (
          <MergedRow
            key={merge.id}
            merge={merge}
            request={requestFor(readReference(merge.properties.loser)?.path)}
            kind={record.kind}
            kinds={kinds}
          />
        ))}
        {/* A former id whose merge record is not among the rows (the read
            failed, or the merge predates the record) is still a fact the
            record carries, so it is listed on its own. */}
        {former
          .filter(
            (id) =>
              !rows.some(
                (m) =>
                  readReference(m.properties.loser)?.path ===
                  recordPath(record.kind, id)
              )
          )
          .map((id) => (
            <li key={id} className="flex items-center gap-3">
              <RecordPill
                kind={kindByIdentity(kinds, record.kind) ? record.kind : ""}
                id={id}
              />
              <span className="text-xs text-muted-foreground">
                {merges.isPending ? "loading its merge…" : "former id"}
              </span>
            </li>
          ))}
      </ul>
      {merges.isError && (
        <p role="alert" className="mt-2 text-xs text-destructive">
          The merge records could not be loaded.{" "}
          <button className="underline" onClick={() => void merges.refetch()}>
            Retry
          </button>
        </p>
      )}
    </section>
  )
}

export function SourcesSection({
  record,
  kinds,
  mappings,
  mappingsPending,
}: {
  record: SubstrateRecord
  kinds: KindInfo[]
  /** The recordmapping declarations the links name, read once per page. */
  mappings: SubstrateRecord[]
  mappingsPending?: boolean
}) {
  const links = record.linkedFrom
  const groups = groupSources(links ?? [], mappings)
  // Distinct records: the index may hold two rows for one source, and one
  // source may reach here under two mappings; either way it is one record.
  const total = new Set((links ?? []).map((l) => l.ref)).size
  const former = record.formerIds ?? []
  const { name } = splitKind(record.kind)

  return (
    <div className="flex min-w-0 flex-col gap-3">
      <div className="flex items-center gap-2">
        <CombineIcon className="size-3.5 text-muted-foreground" />
        <h2 className="text-base font-semibold">Sources</h2>
        {links && (
          <span className="text-sm text-muted-foreground">
            {total.toLocaleString()}{" "}
            {total === 1 ? "record maps" : "records map"} onto this {name}
            {groups.length > 1 &&
              ` through ${groups.length.toLocaleString()} mappings`}
          </span>
        )}
      </div>
      <p className="text-sm text-muted-foreground">
        A source keeps its own record and points at this one through a
        mapping-owned slot; its properties reach here by projection. Nothing
        here is written on this record.
      </p>
      {!links && !former.length && (
        <p className="text-sm text-muted-foreground">
          No mapping targets this kind, so nothing projects onto it. Every value
          here was written directly.
        </p>
      )}
      {groups.map((g) => (
        <Group key={g.mapping} group={g} kinds={kinds} />
      ))}
      {mappingsPending && groups.length > 0 && (
        <p className="text-xs text-muted-foreground">
          Loading the mapping declarations…
        </p>
      )}
      <Merged record={record} kinds={kinds} />
    </div>
  )
}

/** The Manifest tab's footer: the sources, said where a reader expects to
 * find "where did this come from" and does not — the envelope, which cannot
 * carry them because the link lives on the source rows' subject slots. Marked
 * derived, because it is not part of the document and an apply never sends
 * it; one line per mapping, and the way to the Sources section. Nothing
 * renders on a kind no mapping targets: absence there is the envelope being
 * the whole truth. */
export function SourcesFooter({
  record,
  mappings,
  onOpen,
}: {
  record: SubstrateRecord
  mappings: SubstrateRecord[]
  onOpen: () => void
}) {
  const links = record.linkedFrom
  if (!links?.length) return null
  const groups = groupSources(links, mappings)
  const total = new Set(links.map((l) => l.ref)).size
  return (
    <aside
      className="mx-2 mb-2 rounded-lg border border-dashed px-3 py-2 text-xs text-muted-foreground"
      aria-label="derived: sources"
    >
      <p>
        <span className="font-medium text-foreground">
          Derived, not part of the envelope.
        </span>{" "}
        {total.toLocaleString()}{" "}
        {total === 1 ? "source record maps" : "source records map"} onto this
        record and project values into it; an apply never carries them.
      </p>
      <ul className="mt-1 flex flex-wrap gap-x-3 gap-y-0.5">
        {groups.map((g) => (
          <li key={g.mapping} className="data">
            {g.title}{" "}
            <span className="text-muted-foreground/70">
              ({g.members.length.toLocaleString()})
            </span>
          </li>
        ))}
      </ul>
      <button
        type="button"
        className="mt-1 underline underline-offset-4 hover:text-foreground"
        onClick={onOpen}
      >
        Open Sources on the Provenance tab
      </button>
    </aside>
  )
}
