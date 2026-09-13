/** `/apps/$id` and `/apps/$id/$`: one app under the host chrome. The gates
 * run in order before anything mounts: the record itself, the packages its
 * grant names (else the install offer), the SDK major, the `requiresAtLeast`
 * floor, owner provenance (else the review notice with Take over and Open
 * record), then the inputs (an ambiguous or missing one puts the picker where
 * the frame would be). The chrome is driven from bridge state: the title the
 * guest sets, the one primary button it asks for, the back button that walks
 * the app's own history while the splat is non-empty and returns to the
 * launcher when it is. The overflow menu holds Settings (the inputs picker),
 * Open record, Edit source and Reload; the errors strip sits between the
 * header and the frame. */

import { useCallback, useMemo, useRef, useState } from "react"
import { useQuery, useQueryClient } from "@tanstack/react-query"
import { useNavigate, useRouter } from "@tanstack/react-router"
import {
  EllipsisVerticalIcon,
  FileCode2Icon,
  RotateCcwIcon,
  SettingsIcon,
  ShieldAlertIcon,
  SquareArrowOutUpRightIcon,
} from "lucide-react"

import { AppChrome } from "@/components/apps/app-chrome"
import {
  AppFrame,
  OpenRecordLink,
  type AppFrameHandle,
  type FramePhase,
} from "@/components/apps/app-frame"
import { ErrorsStrip } from "@/components/apps/errors"
import { InputBinder } from "@/components/apps/input-binder"
import { ProblemList } from "@/components/apps/problems"
import { useTouchRoot } from "@/components/apps/touch"
import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet"
import { Skeleton } from "@/components/ui/skeleton"
import { toast } from "@/components/ui/toast"
import { useLiveRecords } from "@/hooks/use-live-records"
import { APP_NAME, appQueryOptions } from "@/lib/api/apps"
import { CORE_AUTHORITY, CORE_PACKAGE_NAME } from "@/lib/api/http"
import { kindsQueryOptions } from "@/lib/api/kinds"
import { patchRecord, recordsQueryOptions } from "@/lib/api/records"
import type { SubstrateRecord } from "@/lib/api/types"
import { appSpec, missingPackages } from "@/lib/apps/app-spec"
import { GATED, ownerProvenance } from "@/lib/apps/bridge/host"
import type { PrimaryActionParams } from "@/lib/apps/bridge/protocol"
import { inputStatus, useAppInputs } from "@/lib/apps/inputs"
import { blockingProblems, type AppError } from "@/lib/apps/spec"
import { kindByIdentity } from "@/lib/definition"

/** One page comfortably above any repository's package count. */
const PACKAGES_PAGE = 200

export function ScreenSkeleton({
  title = " ",
  onBack,
}: {
  title?: string
  onBack?: () => void
}) {
  return (
    <AppChrome title={title} onBack={onBack}>
      <div className="flex flex-col">
        {Array.from({ length: 6 }, (_, i) => (
          <div key={i} className="flex flex-col gap-2 border-b px-4 py-3">
            <Skeleton className="h-4 w-2/3" />
            <Skeleton className="h-3 w-1/3" />
          </div>
        ))}
      </div>
    </AppChrome>
  )
}

/** Package identity → the version the repository holds, off the `core/package`
 * collection, for the `requiresAtLeast` floor. */
function packageVersions(records: SubstrateRecord[]): Record<string, number> {
  const out: Record<string, number> = {}
  for (const r of records) {
    const v = r.properties.version
    if (typeof v === "number") out[r.id] = v
  }
  return out
}

export function AppScreen({ id, splat }: { id: string; splat: string }) {
  useTouchRoot()
  const router = useRouter()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const registry = useQuery(kindsQueryOptions)
  const app = useQuery(appQueryOptions(id))
  const packages = useQuery(
    recordsQueryOptions({
      authority: CORE_AUTHORITY,
      package: CORE_PACKAGE_NAME,
      name: "package",
      first: PACKAGES_PAGE,
    })
  )
  const kinds = useMemo(() => registry.data ?? [], [registry.data])
  const versions = useMemo(
    () => packageVersions(packages.data?.records ?? []),
    [packages.data]
  )
  const spec = useMemo(
    () =>
      app.data && registry.data
        ? appSpec(app.data, registry.data, versions)
        : undefined,
    [app.data, registry.data, versions]
  )
  const inputs = useAppInputs(spec, kinds)
  useLiveRecords(inputs.kinds)

  const [title, setTitle] = useState<string>()
  const [primary, setPrimary] = useState<PrimaryActionParams | null>(null)
  const [errors, setErrors] = useState<AppError[]>([])
  const [attempt, setAttempt] = useState(0)
  const [settingsOpen, setSettingsOpen] = useState(false)
  const [takingOver, setTakingOver] = useState(false)
  const frame = useRef<AppFrameHandle>(null)

  const route = splat ? `/${splat}` : ""
  const back = useCallback(() => {
    if (splat) router.history.back()
    else void navigate({ to: "/apps" })
  }, [splat, router, navigate])

  const onPhase = useCallback((phase: FramePhase) => {
    // A fresh mount starts with the chrome the record gives it.
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
    (path: string) => {
      router.history.push(`/apps/${encodeURIComponent(id)}${path}`)
    },
    [router, id]
  )

  if (registry.isPending || app.isPending) {
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

  const record = app.data
  const missing = missingPackages(spec)
  if (missing.length) {
    return (
      <AppChrome title={spec.name} onBack={back}>
        <ProblemList
          title="This app needs a package this repository lacks"
          problems={missing.map((pkg) => ({
            path: "permissions",
            message: `needs ${pkg}`,
            severity: "warning",
          }))}
        />
        <div className="flex justify-center pb-6">
          <Button
            variant="outline"
            onClick={() => void navigate({ to: "/registry" })}
          >
            Open the Registry
          </Button>
        </div>
      </AppChrome>
    )
  }
  const blocking = blockingProblems(spec.problems)
  if (blocking.length) {
    const floor = blocking.some((p) => p.path.startsWith("requiresAtLeast."))
    return (
      <AppChrome title={spec.name} onBack={back}>
        <ProblemList
          title={
            floor
              ? "This app needs a newer package"
              : blocking.some((p) => p.path === "sdk")
                ? "This app needs a newer console"
                : "This app cannot run"
          }
          problems={blocking}
        />
        {floor && (
          <div className="flex justify-center pb-6">
            <Button
              variant="outline"
              onClick={() => void navigate({ to: "/registry" })}
            >
              Open the Registry
            </Button>
          </div>
        )}
      </AppChrome>
    )
  }

  const granted = ownerProvenance(record)

  async function takeOver() {
    setTakingOver(true)
    try {
      let version = record.version
      for (const p of GATED) {
        const value = record.properties[p]
        if (value === undefined) continue
        // Echoing an unchanged value is a no-op the engine skips, so the
        // property is moved and moved back: a required text takes a trailing
        // newline it then loses, an object goes through null.
        const away = typeof value === "string" ? `${value}\n` : null
        const cleared = await patchRecord(
          CORE_AUTHORITY,
          CORE_PACKAGE_NAME,
          APP_NAME,
          id,
          { properties: { [p]: away }, ifVersion: version }
        )
        version = cleared.version
        const restored = await patchRecord(
          CORE_AUTHORITY,
          CORE_PACKAGE_NAME,
          APP_NAME,
          id,
          { properties: { [p]: value }, ifVersion: version }
        )
        version = restored.version
      }
      toast.add({ type: "success", title: `${spec!.name} is yours` })
      void queryClient.invalidateQueries({
        queryKey: ["record", CORE_AUTHORITY, CORE_PACKAGE_NAME, APP_NAME, id],
      })
      void queryClient.invalidateQueries({
        queryKey: ["records", CORE_AUTHORITY, CORE_PACKAGE_NAME, APP_NAME],
      })
    } catch (error) {
      toast.add({
        type: "error",
        title: "Could not take over",
        description: error instanceof Error ? error.message : String(error),
      })
    } finally {
      setTakingOver(false)
    }
  }

  if (!granted) {
    return (
      <AppChrome title={spec.name} onBack={back}>
        <div className="flex flex-col gap-4 px-4 py-6 text-sm">
          <div className="flex items-start gap-3">
            <ShieldAlertIcon className="mt-0.5 size-5 shrink-0 text-warning" />
            <div className="flex min-w-0 flex-col gap-1">
              <p className="font-medium">
                This app needs the owner&apos;s review before it runs.
              </p>
              <p className="text-muted-foreground">
                Its source, modules or grant was not written by the owner, so
                the console mounts nothing and answers no read or write until
                the owner takes it over.
                {spec.description ? ` ${spec.description}` : ""}
              </p>
            </div>
          </div>
          <div className="flex flex-wrap gap-2">
            <Button
              className="h-11 md:h-8"
              disabled={takingOver}
              onClick={() => void takeOver()}
            >
              Take over
            </Button>
            <Button
              variant="outline"
              className="h-11 md:h-8"
              onClick={() =>
                void navigate({
                  to: "/data/$authority/$pkg/$name/$id",
                  params: {
                    authority: CORE_AUTHORITY,
                    pkg: CORE_PACKAGE_NAME,
                    name: APP_NAME,
                    id,
                  },
                })
              }
            >
              Open record
            </Button>
          </div>
          <pre className="max-h-[50vh] overflow-auto rounded border bg-muted/40 p-3 data text-xs leading-5 whitespace-pre">
            {spec.source}
          </pre>
        </div>
      </AppChrome>
    )
  }

  const unresolved = Object.entries(inputs.states)
    .map(([name, state]) => {
      const kind = kindByIdentity(kinds, spec.inputs[name]?.kind ?? "")
      return { name, state, kind, message: inputStatus(name, state, kind) }
    })
    .filter((u) => u.message)
  const binder = unresolved.find(
    (u) => u.state.state === "ambiguous" || u.state.state === "missing"
  )
  const status = unresolved[0]?.message
  const hasInputs = Object.keys(spec.inputs).length > 0

  const menu = (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button
            variant="ghost"
            size="icon"
            className="size-11 shrink-0"
            aria-label="More"
          />
        }
      >
        <EllipsisVerticalIcon className="size-5" />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        {hasInputs && (
          <DropdownMenuItem onClick={() => setSettingsOpen(true)}>
            <SettingsIcon />
            Settings
          </DropdownMenuItem>
        )}
        <DropdownMenuItem
          onClick={() =>
            void navigate({
              to: "/data/$authority/$pkg/$name/$id",
              params: {
                authority: CORE_AUTHORITY,
                pkg: CORE_PACKAGE_NAME,
                name: APP_NAME,
                id,
              },
            })
          }
        >
          <SquareArrowOutUpRightIcon />
          Open record
        </DropdownMenuItem>
        <DropdownMenuItem
          onClick={() =>
            void navigate({
              to: "/data/$authority/$pkg/$name/$id/edit",
              params: {
                authority: CORE_AUTHORITY,
                pkg: CORE_PACKAGE_NAME,
                name: APP_NAME,
                id,
              },
            })
          }
        >
          <FileCode2Icon />
          Edit source
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        <DropdownMenuItem onClick={() => setAttempt((n) => n + 1)}>
          <RotateCcwIcon />
          Reload
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )

  return (
    <AppChrome
      title={title ?? spec.name}
      onBack={back}
      status={status}
      actions={menu}
      primary={
        primary &&
        !binder && (
          <Button
            className="h-12 w-full text-base"
            disabled={primary.enabled === false}
            onClick={() => frame.current?.primaryActionClicked()}
          >
            {primary.label}
          </Button>
        )
      }
    >
      {binder &&
      (binder.state.state === "ambiguous" ||
        binder.state.state === "missing") ? (
        <InputBinder
          app={record}
          name={binder.name}
          input={spec.inputs[binder.name]}
          kind={binder.kind}
          options={binder.state.options}
        />
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
            mode="page"
            route={route}
            inputs={inputs.states}
            attempt={attempt}
            onTitle={setTitle}
            onPrimaryAction={setPrimary}
            onError={onError}
            onPhase={onPhase}
            onNavigatePath={onNavigatePath}
          />
        </>
      )}
      {hasInputs && (
        <Sheet open={settingsOpen} onOpenChange={setSettingsOpen}>
          <SheetContent side="bottom" className="max-h-[85vh] overflow-y-auto">
            <SheetHeader>
              <SheetTitle>Settings</SheetTitle>
              <SheetDescription>
                Which record each input of {spec.name} reads. A pick is written
                to the app record as a binding. <OpenRecordLink id={id} />
              </SheetDescription>
            </SheetHeader>
            {Object.entries(spec.inputs).map(([name, input]) => {
              const state = inputs.states[name]
              const kind = kindByIdentity(kinds, input.kind)
              const current =
                state && "record" in state ? state.record : undefined
              return (
                <InputBinder
                  key={name}
                  app={record}
                  name={name}
                  input={input}
                  kind={kind}
                  options={inputs.candidates[name] ?? []}
                  current={current}
                />
              )
            })}
          </SheetContent>
        </Sheet>
      )}
    </AppChrome>
  )
}
