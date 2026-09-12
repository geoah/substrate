/** The detail layout: one record, the detail's subject, arriving as
 * `ctx.parent` (the screen reads it off the route's record segment). The
 * title, the state badge, the `show` properties by datatype with prose kept
 * as prose, the actions the row admits (a transition only along an arm the
 * machine declares from the current state), and one section per `related`
 * view, mounted through the renderer in card mode with this record as its
 * parent so `via` scopes the read and seeds the create, which is the
 * section's trailing button. The record is re-read on mount and after every
 * action, because a transition moves its version and the next `ifVersion`
 * must carry it. */

import { useMemo } from "react"
import { useQuery, useQueryClient } from "@tanstack/react-query"
import { FileQuestionIcon } from "lucide-react"

import { ActionButton } from "@/components/apps/action-button"
import { ViewBoundary } from "@/components/apps/view-boundary"
import { ViewRenderer } from "@/components/apps/view-renderer"
import { StateBadge } from "@/components/state-badge"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import { Skeleton } from "@/components/ui/skeleton"
import { viewQueryOptions } from "@/lib/api/apps"
import { recordQueryOptions } from "@/lib/api/records"
import type { KindInfo, SubstrateRecord } from "@/lib/api/types"
import { actionApplies, type ActionOutcome } from "@/lib/apps/actions"
import { specOf } from "@/lib/apps/cond"
import { stateSpecOf } from "@/lib/apps/machine"
import type {
  ActionHost,
  LayoutProps,
  RelatedSpec,
  ViewContext,
} from "@/lib/apps/spec"
import { viewSpec } from "@/lib/apps/view-spec"
import { kindByIdentity, splitKind } from "@/lib/definition"
import { titleOf } from "@/lib/apps/referents"
import { DetailValue, EmptyLine, showNames } from "./shared"

export default function DetailLayout({
  spec,
  kind,
  kinds,
  ctx,
  onOpenRecord,
}: LayoutProps) {
  const subject = ctx.parent
  const queryClient = useQueryClient()
  const at = splitKind(subject?.record.kind ?? "")
  const key = recordQueryOptions(
    at.authority,
    at.pkg,
    at.name,
    subject?.record.id ?? ""
  )
  const fresh = useQuery({
    ...key,
    enabled: Boolean(subject),
    initialData: subject?.record,
    staleTime: 0,
  })
  const record = fresh.data ?? subject?.record
  const recordKind = subject?.kind ?? kind
  const names = useMemo(() => showNames(spec, recordKind), [spec, recordKind])

  if (!subject || !record) {
    return (
      <Empty className="py-16">
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <FileQuestionIcon />
          </EmptyMedia>
          <EmptyTitle>No record</EmptyTitle>
          <EmptyDescription>
            A detail shows one record. Open this view from a row.
          </EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  }

  const host: ActionHost = { spec, kind: recordKind, kinds, ctx }
  const state = stateSpecOf(recordKind)
  const stateValue = state ? record.properties[state.name] : undefined
  // In a page mount the screen draws the primary and header actions in its
  // chrome; the detail draws the row-placed ones, its one record being the
  // row. A card or inline mount has no chrome and draws them all.
  const actions = spec.actions.filter((a) => {
    if (ctx.mode === "page" && a.placement !== "row") return false
    return a.verb === "create" || actionApplies(a, record, recordKind)
  })
  // A write moved the record's version; the next `ifVersion` reads it here.
  const onSettled = (outcome?: ActionOutcome) => {
    if (outcome?.ok && outcome.record?.id === record.id) {
      queryClient.setQueryData(key.queryKey, outcome.record)
    }
    void queryClient.invalidateQueries({ queryKey: key.queryKey })
  }

  return (
    <div className="flex flex-col gap-6 px-4 py-4 md:px-6">
      <header className="flex flex-col gap-3">
        <div className="flex items-start gap-3">
          <h2 className="min-w-0 flex-1 text-lg leading-snug font-semibold break-words">
            {titleOf(record)}
          </h2>
          {state && typeof stateValue === "string" && (
            <StateBadge value={stateValue} initial={state.initial} />
          )}
        </div>
        {actions.length > 0 && (
          <div className="flex flex-wrap gap-2">
            {actions.map((action) => (
              <ActionButton
                key={action.name}
                host={host}
                action={action}
                record={action.verb === "create" ? undefined : record}
                className="md:h-9"
                onSettled={onSettled}
              />
            ))}
          </div>
        )}
      </header>

      {names.length > 0 && (
        <dl className="flex flex-col gap-4">
          {names.map((name) => {
            const prop = specOf(recordKind, name)
            if (!prop) return null
            return (
              <div key={name} className="flex min-w-0 flex-col gap-1">
                <dt className="text-xs font-medium tracking-wide text-muted-foreground uppercase">
                  {prop.label}
                </dt>
                <dd className="min-w-0 text-sm">
                  <DetailValue
                    spec={prop}
                    value={record.properties[name]}
                    kinds={kinds}
                  />
                </dd>
              </div>
            )
          })}
        </dl>
      )}

      {spec.related.map((related) => (
        <RelatedSection
          key={related.view}
          related={related}
          parent={{ record, kind: recordKind ?? subject.kind }}
          inputs={ctx.inputs}
          kinds={kinds}
          onOpenRecord={onOpenRecord}
        />
      ))}
    </div>
  )
}

function RelatedSection({
  related,
  parent,
  inputs,
  kinds,
  onOpenRecord,
}: {
  related: RelatedSpec
  parent: { record: SubstrateRecord; kind: KindInfo }
  inputs: ViewContext["inputs"]
  kinds: KindInfo[]
  onOpenRecord: (record: SubstrateRecord) => void
}) {
  const view = useQuery(viewQueryOptions(related.view))
  const spec = useMemo(
    () => (view.data ? viewSpec(view.data, kinds) : undefined),
    [view.data, kinds]
  )
  const kind = spec?.kind ? kindByIdentity(kinds, spec.kind) : undefined
  const ctx = useMemo<ViewContext>(
    () => ({ inputs, parent, mode: "card" }),
    [inputs, parent]
  )
  // In a section mount both primary and header creates are the trailing
  // button; a row-placed create stays with the rows.
  const create = spec?.actions.find(
    (a) => a.verb === "create" && a.placement !== "row"
  )
  const heading = related.heading ?? spec?.name ?? related.view

  return (
    <section className="flex flex-col gap-2" aria-label={heading}>
      <div className="flex min-h-11 items-center gap-2">
        <h3 className="min-w-0 flex-1 text-sm font-semibold">{heading}</h3>
        {create && spec && (
          <ActionButton
            host={{ spec, kind, kinds, ctx }}
            action={create}
            className="md:h-9"
          />
        )}
      </div>
      <ViewBoundary label={related.view}>
        {view.isPending ? (
          <Skeleton className="h-11 w-full" />
        ) : view.error || !spec ? (
          <EmptyLine>
            Could not read the view {related.view}
            {view.error ? `: ${view.error.message}` : ""}
          </EmptyLine>
        ) : (
          <div className="overflow-hidden rounded-xl border bg-card">
            <ViewRenderer
              spec={spec}
              kinds={kinds}
              ctx={ctx}
              onOpenRecord={onOpenRecord}
            />
          </div>
        )}
      </ViewBoundary>
    </section>
  )
}
