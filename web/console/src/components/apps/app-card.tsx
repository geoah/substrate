/** An app mounted where it attaches, outside its own chrome: a card on the
 * overview (`attach: home`), a card on a record page (`attach: record`, the
 * record handed in through the bridge and the card shown only where `via`
 * or the read kinds put it), or the body of a tab on a kind's page
 * (`attach: browse`, filling the tab). The card's own header is the app's
 * name, a door to its page; the frame is inline and sized by what the
 * document reports, up to 60 vh; the guest's primary button, when it asks
 * for one, sits under the frame. Each mount holds its own errors strip and
 * its own boundary, so a broken app takes its rectangle and nothing beside
 * it.
 *
 * The page hands in the row as the LIST served it, which carries no
 * `propertyMeta`; provenance is read off the single-record read, the one
 * wire surface that does, so nothing mounts until that read has landed. */

import { useCallback, useMemo, useRef, useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { Link, useNavigate } from "@tanstack/react-router"

import { AppBoundary } from "@/components/apps/app-boundary"
import {
  AppFrame,
  type AppFrameHandle,
  type FramePhase,
} from "@/components/apps/app-frame"
import { ErrorsStrip } from "@/components/apps/errors"
import { ProblemStrip } from "@/components/apps/problems"
import { Button } from "@/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { useLiveRecords } from "@/hooks/use-live-records"
import { appQueryOptions } from "@/lib/api/apps"
import { kindsQueryOptions } from "@/lib/api/kinds"
import type { SubstrateRecord } from "@/lib/api/types"
import { appSpec, attachesToRecord } from "@/lib/apps/app-spec"
import type { PrimaryActionParams } from "@/lib/apps/bridge/protocol"
import { useAppInputs } from "@/lib/apps/inputs"
import { blockingProblems, type AppError } from "@/lib/apps/spec"

export function AppCard({
  app,
  at,
  attachedRecord,
}: {
  /** The app record, as the list served it. */
  app: SubstrateRecord
  at: "home" | "record" | "browse"
  /** The record a `record` card is mounted on. */
  attachedRecord?: SubstrateRecord
}) {
  const navigate = useNavigate()
  const registry = useQuery(kindsQueryOptions)
  const single = useQuery(appQueryOptions(app.id))
  const record = single.data
  const kinds = useMemo(() => registry.data ?? [], [registry.data])
  const spec = useMemo(
    () =>
      record && registry.data ? appSpec(record, registry.data) : undefined,
    [record, registry.data]
  )
  const inputs = useAppInputs(spec, kinds)
  useLiveRecords(inputs.kinds)

  const [title, setTitle] = useState<string>()
  const [primary, setPrimary] = useState<PrimaryActionParams | null>(null)
  const [errors, setErrors] = useState<AppError[]>([])
  const frame = useRef<AppFrameHandle>(null)
  const onPhase = useCallback((phase: FramePhase) => {
    if (phase === "booting") {
      setTitle(undefined)
      setPrimary(null)
      setErrors([])
    }
  }, [])
  const onError = useCallback(
    (error: AppError) => setErrors((prev) => [...prev, error]),
    []
  )
  const onNavigatePath = useCallback(
    (path: string) =>
      void navigate({
        to: "/apps/$id/$",
        params: { id: app.id, _splat: path.replace(/^\//, "") },
      }),
    [navigate, app.id]
  )

  if (!spec || !record) return <Skeleton className="h-32 rounded-xl" />
  if (
    at === "record" &&
    (!attachedRecord || !attachesToRecord(spec, kinds, attachedRecord.kind))
  ) {
    return null
  }

  const blocking = blockingProblems(spec.problems)
  const body = (
    <AppBoundary label={spec.id}>
      {blocking.length > 0 ? (
        <ProblemStrip problems={blocking} />
      ) : (
        <>
          <ErrorsStrip
            errors={errors}
            source={spec.source}
            modules={spec.modules}
            onClear={() => setErrors([])}
          />
          <AppFrame
            ref={frame}
            spec={spec}
            record={record}
            kinds={kinds}
            mode={at === "browse" ? "page" : "card"}
            attachedRecord={at === "record" ? attachedRecord : undefined}
            inputs={inputs.states}
            onTitle={setTitle}
            onPrimaryAction={setPrimary}
            onError={onError}
            onPhase={onPhase}
            onNavigatePath={onNavigatePath}
          />
          {primary && (
            <div className="shrink-0 border-t px-4 py-3">
              <Button
                className="w-full md:w-auto"
                disabled={primary.enabled === false}
                onClick={() => frame.current?.primaryActionClicked()}
              >
                {primary.label}
              </Button>
            </div>
          )}
        </>
      )}
    </AppBoundary>
  )

  if (at === "browse") {
    return <div className="flex min-h-0 flex-1 flex-col">{body}</div>
  }

  return (
    <Card size="sm" className="min-w-0 gap-2">
      <CardHeader>
        <CardTitle>
          <Link
            to="/apps/$id"
            params={{ id: spec.id }}
            className="underline-offset-4 hover:underline"
          >
            {title ?? spec.name}
          </Link>
        </CardTitle>
        {spec.description && (
          <CardDescription>{spec.description}</CardDescription>
        )}
      </CardHeader>
      <CardContent className="px-0">{body}</CardContent>
    </Card>
  )
}
