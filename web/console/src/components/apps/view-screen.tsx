/** `/views/$id` and `/views/$id/$record`: one view under the host chrome, the
 * same chrome an app screen gets, so a view needs no app to render. The
 * record segment scopes the view through `via`, is a detail's subject, or
 * opens that record's sheet (use-screen-record.ts). The chrome's one primary
 * button is the view's `primary` action, header actions trail the title (on
 * a detail both act on the subject, `chromeActions`), and the facet
 * selection in the URL travels to the layout through the context, which is
 * what makes the chips live on this mount. */

import { useMemo } from "react"
import { useQuery } from "@tanstack/react-query"
import { useNavigate } from "@tanstack/react-router"

import { ActionButton } from "@/components/apps/action-button"
import { AppChrome } from "@/components/apps/app-chrome"
import { ProblemList } from "@/components/apps/problems"
import { MissingRecordSheet, RecordSheet } from "@/components/apps/record-sheet"
import { useTouchRoot } from "@/components/apps/touch"
import {
  chromeActions,
  useBack,
  useScreenRecord,
} from "@/components/apps/use-screen-record"
import { ViewRenderer } from "@/components/apps/view-renderer"
import { Skeleton } from "@/components/ui/skeleton"
import { viewQueryOptions } from "@/lib/api/apps"
import { kindsQueryOptions } from "@/lib/api/kinds"
import { useFacetSelection } from "@/lib/apps/facets"
import { useFloorProblems } from "@/lib/apps/queries"
import { titleOf } from "@/lib/apps/referents"
import type { ViewContext } from "@/lib/apps/spec"
import { viewSpec } from "@/lib/apps/view-spec"
import { kindByIdentity } from "@/lib/definition"

/** The rows a screen shows while what it needs is on its way: the chrome is
 * up, the body is not. */
export function SkeletonRows() {
  return (
    <div className="flex flex-col">
      {Array.from({ length: 6 }, (_, i) => (
        <div key={i} className="flex flex-col gap-2 border-b px-4 py-3">
          <Skeleton className="h-4 w-2/3" />
          <Skeleton className="h-3 w-1/3" />
        </div>
      ))}
    </div>
  )
}

export function ScreenSkeleton({
  title = " ",
  onBack,
}: {
  title?: string
  onBack?: () => void
}) {
  return (
    <AppChrome title={title} onBack={onBack}>
      <SkeletonRows />
    </AppChrome>
  )
}

export function ViewScreen({
  id,
  segment,
}: {
  id: string
  /** The decoded record segment: `<kind>/<id>`, or a bare id. */
  segment?: string
}) {
  useTouchRoot()
  const navigate = useNavigate()
  const registry = useQuery(kindsQueryOptions)
  const view = useQuery(viewQueryOptions(id))
  const kinds = registry.data ?? []
  const spec = useMemo(
    () =>
      view.data && registry.data
        ? viewSpec(view.data, registry.data)
        : undefined,
    [view.data, registry.data]
  )
  const kind = spec?.kind ? kindByIdentity(kinds, spec.kind) : undefined
  const floors = useFloorProblems(spec)
  const screen = useScreenRecord({
    spec,
    kind,
    kinds,
    segment,
    toRecord: (record, nav) =>
      void navigate({
        to: "/views/$id/$record",
        params: { id, record },
        ...nav,
      }),
    toView: (viewId, record) =>
      void navigate({
        to: "/views/$id/$record",
        params: { id: viewId, record },
      }),
    clear: (nav) => void navigate({ to: "/views/$id", params: { id }, ...nav }),
  })
  const back = useBack(screen)
  const { selection } = useFacetSelection(spec)
  const ctx = useMemo<ViewContext>(
    () => ({
      inputs: {},
      parent: screen.parent,
      mode: "page",
      facets: selection,
    }),
    [screen.parent, selection]
  )

  if (registry.isPending || view.isPending) {
    return <ScreenSkeleton onBack={back} />
  }
  if (view.isError || !view.data || !spec) {
    return (
      <AppChrome title="View" onBack={back}>
        <ProblemList
          title="No such view"
          problems={[
            {
              path: id,
              message: view.error?.message ?? "not found",
              severity: "error",
            },
          ]}
        />
      </AppChrome>
    )
  }
  if (screen.parentError) {
    return (
      <AppChrome title={spec.name} onBack={back}>
        <ProblemList
          title="No such record"
          problems={[
            {
              path: segment ?? "",
              message: screen.parentError.message,
              severity: "error",
            },
          ]}
        />
      </AppChrome>
    )
  }
  if (screen.parentPending) {
    return <ScreenSkeleton title={spec.name} onBack={back} />
  }

  const chrome = chromeActions({ spec, kind, kinds, ctx }, screen)
  // The chrome's verbs are withheld below a floor the view declares, as the
  // rows are; the renderer says the shortfall.
  const short = !floors || floors.length > 0
  return (
    <AppChrome
      title={spec.name}
      subtitle={
        screen.parent ? titleOf(screen.parent.record) : spec.description
      }
      onBack={back}
      actions={
        chrome.header.length && !short
          ? chrome.header.map((action) => (
              <ActionButton
                key={action.name}
                host={chrome.host}
                action={action}
                record={chrome.record}
                variant="ghost"
                compact
              />
            ))
          : undefined
      }
      primary={
        chrome.primary &&
        !short && (
          <ActionButton
            host={chrome.host}
            action={chrome.primary}
            record={chrome.record}
            variant="default"
            className="h-12 w-full text-base"
          />
        )
      }
    >
      <ViewRenderer
        spec={spec}
        kinds={kinds}
        ctx={ctx}
        onOpenRecord={screen.openRecord}
      />
      {screen.sheetRecord && (
        <RecordSheet
          host={{ spec, kind, kinds, ctx }}
          record={screen.sheetRecord}
          open
          onOpenChange={(open) => !open && screen.closeSheet()}
        />
      )}
      {screen.sheetError && (
        <MissingRecordSheet
          segment={screen.sheetSegment ?? ""}
          message={screen.sheetError.message}
          open
          onOpenChange={(open) => !open && screen.closeSheet()}
        />
      )}
    </AppChrome>
  )
}
