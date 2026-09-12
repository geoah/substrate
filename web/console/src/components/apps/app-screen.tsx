/** `/apps/$id`, `/apps/$id/$screen` and `/apps/$id/$screen/$record`: one
 * screen of an app under the host chrome, two screens as a segmented control
 * and three to five as a tab bar. The app's inputs resolve here
 * (lib/apps/inputs.ts) and travel to the view as `ctx.inputs`; an ambiguous
 * or missing input puts the picker where the view would be, and an input or
 * a token that cannot resolve is a line under the header rather than a
 * blank screen. `/apps/$id` alone lands on the first screen. */

import { useEffect, useMemo } from "react"
import { useQuery } from "@tanstack/react-query"
import { useNavigate } from "@tanstack/react-router"

import { ActionButton } from "@/components/apps/action-button"
import { AppChrome } from "@/components/apps/app-chrome"
import { InputBinder } from "@/components/apps/input-binder"
import { ProblemList } from "@/components/apps/problems"
import { MissingRecordSheet, RecordSheet } from "@/components/apps/record-sheet"
import { useTouchRoot } from "@/components/apps/touch"
import { useBack, useScreenRecord } from "@/components/apps/use-screen-record"
import { ViewRenderer } from "@/components/apps/view-renderer"
import { ScreenSkeleton } from "@/components/apps/view-screen"
import { useLiveRecords } from "@/hooks/use-live-records"
import { appQueryOptions, viewsQueryOptions } from "@/lib/api/apps"
import { kindsQueryOptions } from "@/lib/api/kinds"
import { appSpec } from "@/lib/apps/app-spec"
import { useFacetSelection } from "@/lib/apps/facets"
import { inputStatus, useAppInputs } from "@/lib/apps/inputs"
import { titleOf } from "@/lib/apps/referents"
import {
  blockingProblems,
  type ActionHost,
  type ViewContext,
} from "@/lib/apps/spec"
import { substituteFilter } from "@/lib/apps/tokens"
import { viewSpec } from "@/lib/apps/view-spec"
import { kindByIdentity } from "@/lib/definition"

export function AppScreen({
  id,
  screen: screenName,
  segment,
}: {
  id: string
  screen?: string
  segment?: string
}) {
  useTouchRoot()
  const navigate = useNavigate()
  const registry = useQuery(kindsQueryOptions)
  const app = useQuery(appQueryOptions(id))
  const views = useQuery(viewsQueryOptions())
  const kinds = registry.data ?? []
  const viewRecords = useMemo(() => views.data?.records ?? [], [views.data])
  const spec = useMemo(
    () =>
      app.data && views.data && registry.data
        ? appSpec(app.data, viewRecords, registry.data)
        : undefined,
    [app.data, views.data, viewRecords, registry.data]
  )
  const current =
    spec?.screens.find((s) => s.name === screenName) ?? spec?.screens[0]

  // A bare `/apps/$id` is the first screen; the address follows.
  useEffect(() => {
    if (!screenName && current) {
      void navigate({
        to: "/apps/$id/$screen",
        params: { id, screen: current.name },
        replace: true,
      })
    }
  }, [screenName, current, id, navigate])

  const viewRecord = viewRecords.find((v) => v.id === current?.view)
  const vspec = useMemo(
    () =>
      viewRecord && registry.data
        ? viewSpec(viewRecord, registry.data)
        : undefined,
    [viewRecord, registry.data]
  )
  const kind = vspec?.kind ? kindByIdentity(kinds, vspec.kind) : undefined
  const inputs = useAppInputs(spec, kinds)
  useLiveRecords(inputs.kinds)

  const screenRecord = useScreenRecord({
    spec: vspec,
    kind,
    kinds,
    segment,
    toRecord: (record, nav) =>
      void navigate({
        to: "/apps/$id/$screen/$record",
        params: { id, screen: current?.name ?? "", record },
        ...nav,
      }),
    toView: (viewId, record) =>
      void navigate({
        to: "/views/$id/$record",
        params: { id: viewId, record },
      }),
    clear: (nav) =>
      void navigate({
        to: "/apps/$id/$screen",
        params: { id, screen: current?.name ?? "" },
        ...nav,
      }),
  })
  const back = useBack(screenRecord)
  const { selection } = useFacetSelection(vspec)
  const ctx = useMemo<ViewContext>(
    () => ({
      inputs: inputs.records,
      parent: screenRecord.parent,
      mode: "page",
      facets: selection,
    }),
    [inputs.records, screenRecord.parent, selection]
  )

  if (registry.isPending || app.isPending || views.isPending) {
    return <ScreenSkeleton onBack={back} />
  }
  if (app.isError || !app.data || !spec) {
    return (
      <AppChrome title="App" onBack={back}>
        <ProblemList
          title="No such app"
          problems={[
            {
              path: id,
              message: app.error?.message ?? "not found",
              severity: "error",
            },
          ]}
        />
      </AppChrome>
    )
  }
  const blocking = blockingProblems(spec.problems)
  if (blocking.length || !current || !vspec) {
    return (
      <AppChrome title={spec.name} onBack={back}>
        <ProblemList
          title="This app cannot render"
          problems={
            blocking.length
              ? blocking
              : [
                  {
                    path: "screens",
                    message: current
                      ? `no view ${current.view}`
                      : "an app has at least one screen",
                    severity: "error",
                  },
                ]
          }
        />
      </AppChrome>
    )
  }

  const unresolved = Object.entries(inputs.states)
    .map(([name, state]) => ({
      name,
      state,
      kind: kindByIdentity(kinds, spec.inputs[name]?.kind ?? ""),
      message: inputStatus(
        name,
        state,
        kindByIdentity(kinds, spec.inputs[name]?.kind ?? "")
      ),
    }))
    .filter((u) => u.message)
  const binder = unresolved.find(
    (u) => u.state.state === "ambiguous" || u.state.state === "missing"
  )
  const tokenProblems = substituteFilter(vspec.filter, ctx).problems
  const status = unresolved[0]?.message ?? tokenProblems[0]?.message

  const host: ActionHost = { spec: vspec, kind, kinds, ctx }
  const primary = vspec.actions.find((a) => a.placement === "primary")
  const header = vspec.actions.filter((a) => a.placement === "header")
  const screens = spec.screens.map((s) => ({
    name: s.name,
    label: s.label,
    icon: s.icon,
  }))

  return (
    <AppChrome
      title={spec.name}
      subtitle={
        screenRecord.parent ? titleOf(screenRecord.parent.record) : undefined
      }
      onBack={back}
      screens={screens}
      screen={current.name}
      onScreen={(name) =>
        void navigate({
          to: "/apps/$id/$screen",
          params: { id, screen: name },
        })
      }
      status={status}
      actions={
        header.length
          ? header.map((action) => (
              <ActionButton
                key={action.name}
                host={host}
                action={action}
                variant="ghost"
                compact
              />
            ))
          : undefined
      }
      primary={
        primary &&
        !binder && (
          <ActionButton
            host={host}
            action={primary}
            variant="default"
            className="h-12 w-full text-base"
          />
        )
      }
    >
      {binder &&
      (binder.state.state === "ambiguous" ||
        binder.state.state === "missing") ? (
        <InputBinder
          app={app.data}
          name={binder.name}
          input={spec.inputs[binder.name]}
          kind={binder.kind}
          options={binder.state.options}
        />
      ) : screenRecord.parentError ? (
        <ProblemList
          title="No such record"
          problems={[
            {
              path: segment ?? "",
              message: screenRecord.parentError.message,
              severity: "error",
            },
          ]}
        />
      ) : (
        <ViewRenderer
          key={current.name}
          spec={vspec}
          kinds={kinds}
          ctx={ctx}
          onOpenRecord={screenRecord.openRecord}
        />
      )}
      {screenRecord.sheetRecord && (
        <RecordSheet
          host={host}
          record={screenRecord.sheetRecord}
          open
          onOpenChange={(open) => !open && screenRecord.closeSheet()}
        />
      )}
      {screenRecord.sheetError && (
        <MissingRecordSheet
          segment={segment ?? ""}
          message={screenRecord.sheetError.message}
          open
          onOpenChange={(open) => !open && screenRecord.closeSheet()}
        />
      )}
    </AppChrome>
  )
}
