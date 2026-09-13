/** `/apps/$id` and `/apps/$id/$`: one app under the host chrome. The gate is
 * `app-gate.ts`, the same one a card passes, and its refusals are drawn here
 * in order: the record itself, the packages the grant names (else the
 * install offer), the SDK major and the `requiresAtLeast` floor, owner
 * provenance (else the review notice with Take over and Open record), then
 * the inputs (an ambiguous or missing one puts the picker where the frame
 * would be). The chrome is driven from bridge state: the title the guest
 * sets, the one primary button it asks for, and the back button, which is
 * put to the guest first (a listener may be dismissing a sheet) and walks
 * the app's own history while the splat is non-empty and returns to the
 * launcher when it is, for a guest that did not take it. The overflow menu
 * holds Settings (the inputs picker), Open record, Edit source and Reload;
 * the errors strip sits between the header and the frame. */

import { useCallback, useRef, useState } from "react"
import { useQueryClient } from "@tanstack/react-query"
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
import { useAppGate } from "@/components/apps/app-gate"
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
import { APP_NAME } from "@/lib/api/apps"
import { CORE_AUTHORITY, CORE_PACKAGE_NAME } from "@/lib/api/http"
import { patchRecord } from "@/lib/api/records"
import { GATED } from "@/lib/apps/bridge/host"
import type { PrimaryActionParams } from "@/lib/apps/bridge/protocol"
import { inputStatus } from "@/lib/apps/inputs"
import type { AppError } from "@/lib/apps/spec"
import { kindByIdentity } from "@/lib/definition"

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

export function AppScreen({ id, splat }: { id: string; splat: string }) {
  useTouchRoot()
  const router = useRouter()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const gate = useAppGate(id)

  const [title, setTitle] = useState<string>()
  const [primary, setPrimary] = useState<PrimaryActionParams | null>(null)
  const [errors, setErrors] = useState<AppError[]>([])
  const [attempt, setAttempt] = useState(0)
  const [settingsOpen, setSettingsOpen] = useState(false)
  const [takingOver, setTakingOver] = useState(false)
  const frame = useRef<AppFrameHandle>(null)

  const route = splat ? `/${splat}` : ""
  // The host's own back, root-aware: the app's history while its path is
  // non-empty and there is an entry of ours to walk back to; the app's root
  // when the path was opened straight (a bare history.back() there leaves
  // the console for whatever came before it); the launcher at the root.
  // What an SDK `host.back()` takes too.
  const hostBack = useCallback(() => {
    if (!splat) void navigate({ to: "/apps" })
    else if (router.history.canGoBack()) router.history.back()
    else void navigate({ to: "/apps/$id", params: { id } })
  }, [splat, router, navigate, id])
  // The header's tap: the guest first, the host only for a guest that did
  // not take it (or a screen with no guest up).
  const back = useCallback(() => {
    const asked = frame.current?.back()
    if (!asked) {
      hostBack()
      return
    }
    void asked.then((handled) => {
      if (!handled) hostBack()
    })
  }, [hostBack])

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

  if (gate.phase === "pending") {
    return <ScreenSkeleton onBack={hostBack} />
  }
  if (gate.phase === "absent") {
    return (
      <AppChrome title="App" onBack={hostBack}>
        <ProblemList
          title="No such app"
          problems={[{ path: id, message: gate.message, severity: "error" }]}
        />
      </AppChrome>
    )
  }

  const { record, spec, kinds, granted, missing, blocking, inputs } = gate
  if (missing.length) {
    return (
      <AppChrome title={spec.name} onBack={hostBack}>
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
  if (blocking.length) {
    const floor = blocking.some((p) => p.path.startsWith("requiresAtLeast"))
    return (
      <AppChrome title={spec.name} onBack={hostBack}>
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
      toast.add({ type: "success", title: `${spec.name} is yours` })
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
      <AppChrome title={spec.name} onBack={hostBack}>
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
      {binder ? (
        <InputBinder
          app={record}
          name={binder.name}
          input={spec.inputs[binder.name]}
          kind={binder.kind}
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
            onBack={hostBack}
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
                  current={current}
                  status={state ? inputStatus(name, state, kind) : undefined}
                />
              )
            })}
          </SheetContent>
        </Sheet>
      )}
    </AppChrome>
  )
}
