/** One tool: what it is, what it is allowed to do, when it runs, who uses
 * it, what it takes and gives back, a way to run it, and how its recent runs
 * went. Technical mode adds the declaration: runtime, permissions, source. */

import {
  useMutation,
  useQueries,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import {
  ArrowUpRight,
  Bot,
  ChevronRight,
  Clock,
  Code,
  Eye,
  Globe,
  Pause,
  Pencil,
  Play,
  RefreshCw,
  TriangleAlert,
  User,
  Webhook,
  Zap,
  type LucideIcon,
} from "lucide-react"
import { Fragment, useState, type ReactNode } from "react"

import { CodeBlock } from "@/components/code-block"
import { ActorRef } from "@/components/identity/actor-ref"
import { IdText } from "@/components/identity/id-text"
import { PageHeader } from "@/components/identity/page-header"
import { DocPage } from "@/components/identity/page-layout"
import { RunIO } from "@/components/tools/run-io"
import { OriginTag, StatusPill, ToolTile } from "@/components/tools/tool-marks"
import { TryIt } from "@/components/tools/try-it"
import {
  agentActor,
  useTools,
  type ToolsModel,
} from "@/components/tools/use-tools"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { toast } from "@/components/ui/toast"
import { useTechnicalDetails } from "@/hooks/use-console-preferences"
import { setTriggerEnabled } from "@/lib/api/functions"
import { LLM_PACKAGE } from "@/lib/api/http"
import type { TriggerStatus } from "@/lib/api/types"
import {
  requestSync,
  triggerRunsQueryOptions,
  triggerStatusesQueryOptions,
  wakeTriggers,
} from "@/lib/api/sync"
import {
  PERMISSION_NONE,
  TRIGGER_RUN_KIND,
  agoWords,
  argumentLabel,
  argumentTypeWords,
  canTryIt,
  isPaused,
  isSync,
  isoDurationWords,
  permissionWords,
  permissionsYaml,
  runFromTriggerRun,
  sentence,
  sortRuns,
  toolDescription,
  toolName,
  toolStarts,
  toolStatus,
  tookWords,
  triggerProgress,
  type PermissionWords,
  type StartKind,
  type Tool,
  type ToolRun,
} from "@/lib/tools"
import { cn } from "@/lib/utils"
import { toolRoute } from "@/router"

export function ToolPage() {
  const { authority, pkg, name } = toolRoute.useParams()
  const ref = `${authority}/${pkg}/${name}`
  const model = useTools(ref)
  const [technical] = useTechnicalDetails()
  const tool = model.tools.find((t) => t.ref === ref)

  if (model.isPending) {
    return (
      <DocPage>
        <Skeleton className="h-11 w-72" />
        <Skeleton className="mt-6 h-24 w-full" />
      </DocPage>
    )
  }
  if (!tool) {
    return (
      <DocPage>
        <PageHeader
          title="No such tool"
          description={
            model.error
              ? "Couldn’t load your tools. Check your connection and reload the page."
              : "Nothing in your substrate is called this. It may have been removed with the provider or sample that brought it."
          }
        />
        <p className="mt-4 text-[13px]">
          <Link to="/tools" className="text-primary-text hover:underline">
            See all tools
          </Link>
        </p>
        {technical && <IdText value={ref} className="mt-3" />}
      </DocPage>
    )
  }
  return <ToolDoc tool={tool} model={model} technical={technical} />
}

function ToolDoc({
  tool,
  model,
  technical,
}: {
  tool: Tool
  model: ToolsModel
  technical: boolean
}) {
  const queryClient = useQueryClient()
  const triggerRuns = useQueries({
    queries: tool.triggers.map((t) => triggerRunsQueryOptions(t.id, 20)),
  })
  const agentRuns = model.runsOf(tool).filter((r) => r.by === "agent")
  const runs = sortRuns([
    ...new Map(
      triggerRuns
        .flatMap((q) => (q.data ?? []).map(runFromTriggerRun))
        .map((r) => [r.key, r])
    ).values(),
    ...agentRuns,
  ]).slice(0, 20)
  const runsPending = triggerRuns.some((q) => q.isPending)
  const waitingFor = model.waitingFor(tool)
  const paused = isPaused(tool)
  const usage = model.usageOf(tool)
  const status = toolStatus(tool, runs[0], { waitingFor, usage })
  const sync = isSync(tool)
  const accounts = model.accountsOf(tool)

  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: ["records"] })
    void queryClient.invalidateQueries({ queryKey: ["triggers"] })
    void queryClient.invalidateQueries({ queryKey: ["sync"] })
  }

  const pause = useMutation({
    mutationFn: async (enabled: boolean) => {
      for (const t of tool.triggers) await setTriggerEnabled(t.id, enabled)
    },
    onSuccess: (_, enabled) => {
      refresh()
      toast.add({
        type: "success",
        title: enabled
          ? `${toolName(tool)} is running again.`
          : `${toolName(tool)} is paused.`,
      })
    },
    onError: (err) =>
      toast.add({
        type: "error",
        title: "Couldn’t change it.",
        description: (err as Error).message,
      }),
  })

  const syncNow = useMutation({
    mutationFn: async () => {
      for (const a of accounts) await requestSync(a)
      const ids = tool.triggers
        .filter((t) => t.enabled && t.source.arm === "record")
        .map((t) => t.id)
      return wakeTriggers(ids)
    },
    onSuccess: () => {
      refresh()
      toast.add({ type: "success", title: "Sync started." })
    },
    onError: (err) =>
      toast.add({
        type: "error",
        title: "Couldn’t start the sync.",
        description: (err as Error).message,
      }),
  })

  const actions =
    tool.triggers.length > 0 ? (
      <>
        <Button
          variant="outline"
          size="sm"
          disabled={pause.isPending}
          onClick={() => pause.mutate(paused)}
        >
          {paused ? (
            <Play className="size-3.5" />
          ) : (
            <Pause className="size-3.5" />
          )}
          {paused ? "Resume" : "Pause"}
        </Button>
        {sync && (
          <Button
            size="sm"
            disabled={paused || !accounts.length || syncNow.isPending}
            title={
              !accounts.length
                ? "Connect an account first"
                : paused
                  ? "Resume it first"
                  : undefined
            }
            onClick={() => syncNow.mutate()}
          >
            <RefreshCw
              className={cn("size-3.5", syncNow.isPending && "animate-spin")}
            />
            Sync now
          </Button>
        )}
      </>
    ) : undefined

  const provider = tool.origin.kind === "provider" ? tool.origin : undefined
  const starts = toolStarts(tool, model.label)
  const perms = permissionWords(tool, model.label, (ref) => toolName(ref))

  return (
    <DocPage className="pb-16">
      <PageHeader
        glyph={<ToolTile tool={tool.ref} size="lg" />}
        title={toolName(tool)}
        meta={
          <>
            <OriginTag origin={tool.origin} />
            {status && <StatusPill status={status} />}
            {technical && <IdText value={tool.ref} copy />}
          </>
        }
        description={toolDescription(tool)}
        actions={actions}
      />

      {paused ? (
        <Callout
          title="Paused"
          action={
            <button
              type="button"
              className="font-medium text-primary-text hover:underline"
              onClick={() => pause.mutate(true)}
            >
              Resume it
            </button>
          }
        >
          It won’t run on its own until you resume it. Nothing it brought in is
          lost.
        </Callout>
      ) : waitingFor && provider ? (
        <Callout
          title={`Waiting for ${waitingFor.name}`}
          action={
            <Link
              to="/providers/$authority/$pkg"
              params={{
                authority: tool.authority,
                pkg: tool.pkg,
              }}
              className="font-medium text-primary-text hover:underline"
            >
              Finish setting up {waitingFor.name}
            </Link>
          }
        >
          This sync starts on its own once {waitingFor.name} is set up and an
          account is connected.
        </Callout>
      ) : null}

      <Section
        title="What it’s allowed to do"
        hint="It can’t do anything outside this."
      >
        <Capabilities words={perms} />
      </Section>

      <Section title="When it runs">
        {starts.length ? (
          <div className="overflow-hidden rounded-[10px] border">
            {starts.map((s, i) => (
              <div
                key={`${s.kind}:${s.trigger ?? i}`}
                className="grid grid-cols-[minmax(0,180px)_minmax(0,1fr)_auto] items-center gap-3 border-b px-3 py-2.5 text-[13px] last:border-b-0 max-sm:grid-cols-[minmax(0,1fr)]"
              >
                <span className="inline-flex items-center gap-1.5 font-medium">
                  <StartIcon kind={s.kind} />
                  {s.label}
                </span>
                <span className={cn(!s.enabled && "text-faint")}>
                  {s.detail}
                  {!s.enabled && " · paused"}
                </span>
                <span className="text-right">
                  {technical && s.trigger && <IdText value={s.trigger} />}
                </span>
              </div>
            ))}
          </div>
        ) : (
          <p className="text-[13px] text-faint">
            Nothing runs it yet: no agent lists it and no trigger calls it.
          </p>
        )}
      </Section>

      {tool.uses.length > 0 && (
        <Section title="Used by">
          <div className="flex flex-wrap gap-2">
            {[...new Set(tool.uses.map((u) => u.agent))].map((agent) => (
              <Link
                key={agent}
                to="/agents/$id"
                params={{ id: agent }}
                className="inline-flex items-center rounded-full border px-2.5 py-1 text-[13px] no-underline hover:border-border-strong hover:bg-panel"
              >
                <ActorRef actor={agentActor(agent)} link={false} />
              </Link>
            ))}
          </div>
        </Section>
      )}

      <Section title="What it needs, and what it gives back">
        <InputsOutputs tool={tool} technical={technical} />
      </Section>

      {canTryIt(tool) && (
        <Section
          title="Try it"
          hint="Runs it once, now. What it changes is recorded as its work."
        >
          <TryIt tool={tool.ref} args={tool.arguments} technical={technical} />
        </Section>
      )}

      <Section
        title="Recent runs"
        hint={runs.length > 1 ? `the last ${runs.length}` : undefined}
      >
        {runsPending ? (
          <Skeleton className="h-20 w-full" />
        ) : runs.length ? (
          <RunsTable runs={runs} technical={technical} />
        ) : (
          <p className="text-[13px] text-faint">It hasn’t run yet.</p>
        )}
      </Section>

      {technical && <Developer tool={tool} />}
    </DocPage>
  )
}

// ── pieces ──────────────────────────────────────────────────────────────────

function Section({
  title,
  hint,
  children,
}: {
  title: string
  hint?: string
  children: ReactNode
}) {
  return (
    <section>
      <div className="mt-8 mb-2.5 flex flex-wrap items-baseline gap-x-2 gap-y-0.5">
        <h2 className="text-[15px] font-semibold tracking-[-0.01em]">
          {title}
        </h2>
        {hint && <span className="text-[12.5px] text-faint">{hint}</span>}
      </div>
      {children}
    </section>
  )
}

function Callout({
  title,
  action,
  children,
}: {
  title: string
  action?: ReactNode
  children: ReactNode
}) {
  return (
    <div
      role="note"
      className="mt-5 flex items-start gap-2.5 rounded-[8px] bg-warn-soft px-3.5 py-3 text-[13px]"
    >
      <TriangleAlert className="mt-0.5 size-4 shrink-0 text-warning" />
      <div>
        <div className="font-semibold">{title}</div>
        <div className="text-muted-foreground">
          {children} {action}
        </div>
      </div>
    </div>
  )
}

const CAPS: {
  key: keyof PermissionWords
  label: string
  icon: LucideIcon
}[] = [
  { key: "sees", label: "Can see", icon: Eye },
  { key: "changes", label: "Can change", icon: Pencil },
  { key: "internet", label: "Internet", icon: Globe },
  { key: "asks", label: "Can ask", icon: Bot },
]

function Capabilities({ words }: { words: PermissionWords }) {
  return (
    <div className="grid grid-cols-[repeat(auto-fit,minmax(170px,1fr))] gap-2.5">
      {CAPS.map(({ key, label, icon: Icon }) => {
        const value = words[key]
        return (
          <div
            key={key}
            data-slot="capability"
            className="flex flex-col gap-1 rounded-[10px] border px-3.5 py-3"
          >
            <div className="flex items-center gap-2 text-xs font-medium text-faint">
              <Icon className="size-3.5" />
              {label}
            </div>
            <div
              className={cn(
                "text-[13.5px]",
                value ? "text-foreground" : "text-faint"
              )}
            >
              {value ?? PERMISSION_NONE[key]}
            </div>
          </div>
        )
      })}
    </div>
  )
}

const START_ICONS: Record<StartKind, LucideIcon> = {
  agent: Bot,
  schedule: Clock,
  change: Zap,
  webhook: Webhook,
  you: User,
}

function StartIcon({ kind }: { kind: StartKind }) {
  const Icon = START_ICONS[kind]
  return <Icon className="size-3.5 text-muted-foreground" />
}

function InputsOutputs({
  tool,
  technical,
}: {
  tool: Tool
  technical: boolean
}) {
  if (!tool.arguments.length && !tool.returns.length) {
    return (
      <p className="text-[13px] text-faint">
        {tool.runtime === "host" || isSync(tool)
          ? "Nothing you fill in: it works from what it’s handed when it runs."
          : "It doesn’t say. It takes whatever it’s given."}
      </p>
    )
  }
  const row =
    "grid grid-cols-[minmax(0,160px)_minmax(0,1fr)_auto] items-center gap-3 border-b px-3 py-2.5 text-[13px] last:border-b-0 max-sm:grid-cols-[minmax(0,110px)_minmax(0,1fr)]"
  return (
    <div className="overflow-hidden rounded-[10px] border">
      {tool.arguments.map((a) => (
        <div key={`in:${a.name}`} className={row}>
          <span className="font-medium break-words">
            {argumentLabel(a.name)}
            {technical && (
              <span className="mt-0.5 block font-mono text-[11px] font-normal text-faint">
                {a.name}: {argumentTypeWords(a)}
              </span>
            )}
          </span>
          <span className="text-muted-foreground">
            {a.description ? sentence(a.description) : "—"}
          </span>
          <span className="text-xs text-faint max-sm:hidden">
            {a.required ? "needed" : "optional"}
          </span>
        </div>
      ))}
      {tool.returns.map((r) => (
        <div key={`out:${r.name}`} className={cn(row, "bg-panel")}>
          <span className="inline-flex items-start gap-1.5 font-medium">
            <ArrowUpRight className="mt-0.5 size-3.5 shrink-0 text-muted-foreground" />
            <span>
              {argumentLabel(r.name)}
              {technical && (
                <span className="mt-0.5 block font-mono text-[11px] font-normal text-faint">
                  {r.name}: {argumentTypeWords(r)}
                </span>
              )}
            </span>
          </span>
          <span className="text-muted-foreground">
            {r.description ? sentence(r.description) : "—"}
          </span>
          <span className="text-xs text-faint max-sm:hidden">gives back</span>
        </div>
      ))}
    </div>
  )
}

const RUN_STATUS: Record<
  ToolRun["status"],
  { tone: "ok" | "warn" | "neutral"; label: string }
> = {
  ok: { tone: "ok", label: "Worked" },
  skipped: { tone: "neutral", label: "Skipped" },
  trouble: { tone: "warn", label: "Had trouble" },
}

const BY_WORDS: Record<
  Exclude<ToolRun["by"], "agent">,
  [LucideIcon, string]
> = {
  schedule: [Clock, "Schedule"],
  change: [Zap, "A change"],
  webhook: [Webhook, "A web request"],
  you: [User, "You"],
}

function StartedBy({ run }: { run: ToolRun }) {
  if (run.by === "agent" && run.agent) {
    return <ActorRef actor={agentActor(run.agent)} link={false} />
  }
  const [Icon, words] = BY_WORDS[run.by === "agent" ? "change" : run.by]
  return (
    <span className="inline-flex items-center gap-1.5">
      <Icon className="size-3.5 text-muted-foreground" />
      {words}
    </span>
  )
}

function RunsTable({
  runs,
  technical,
}: {
  runs: ToolRun[]
  technical: boolean
}) {
  const [open, setOpen] = useState<ReadonlySet<string>>(new Set())
  const toggle = (key: string) =>
    setOpen((prev) => {
      const next = new Set(prev)
      if (!next.delete(key)) next.add(key)
      return next
    })
  const grid =
    "grid grid-cols-[110px_minmax(0,1.2fr)_minmax(0,2fr)_70px_100px] items-center gap-2.5 px-3 max-md:grid-cols-[90px_minmax(0,1fr)_100px]"
  return (
    <div
      className="overflow-hidden rounded-[10px] border"
      role="table"
      aria-label="Recent runs"
    >
      <div role="row" className={cn(grid, "h-8 bg-panel text-xs text-faint")}>
        <span role="columnheader">When</span>
        <span role="columnheader" className="max-md:hidden">
          Started by
        </span>
        <span role="columnheader">What happened</span>
        <span role="columnheader" className="max-md:hidden">
          Took
        </span>
        <span role="columnheader">
          <span className="sr-only">Status</span>
        </span>
      </div>
      {runs.map((r) => {
        const s = RUN_STATUS[r.status]
        const expanded = technical && open.has(r.key)
        return (
          <Fragment key={r.key}>
            <div
              role="row"
              className={cn(grid, "min-h-10 border-t py-2 text-[13px]")}
            >
              <span role="cell" className="text-muted-foreground" title={r.at}>
                {agoWords(r.at)}
              </span>
              <span role="cell" className="min-w-0 max-md:hidden">
                <StartedBy run={r} />
              </span>
              <span
                role="cell"
                className="min-w-0 break-words"
                title={r.reason}
              >
                {r.happened}
                {technical && r.trigger && (
                  <span className="mt-0.5 block">
                    <IdText value={r.trigger} />
                  </span>
                )}
                {technical && (
                  <button
                    type="button"
                    aria-expanded={expanded}
                    onClick={() => toggle(r.key)}
                    className="mt-0.5 flex cursor-pointer items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
                  >
                    <ChevronRight
                      aria-hidden
                      className={cn(
                        "size-3 transition-transform",
                        expanded && "rotate-90"
                      )}
                    />
                    Input and output
                  </button>
                )}
              </span>
              <span role="cell" className="text-muted-foreground max-md:hidden">
                {tookWords(r.tookMs)}
              </span>
              <span role="cell">
                <StatusPill status={s} />
              </span>
            </div>
            {expanded && (
              <div role="row" className="border-t bg-panel px-3 py-3">
                <div role="cell">
                  <RunIO run={r} />
                </div>
              </div>
            )}
          </Fragment>
        )
      })}
    </div>
  )
}

function Developer({ tool }: { tool: Tool }) {
  const statuses = useQuery({
    ...triggerStatusesQueryOptions,
    enabled: tool.triggers.length > 0,
  })
  const timeout = isoDurationWords(tool.record.properties.timeout)
  const source = tool.record.properties.source
  return (
    <Section title="Developer">
      <div className="flex flex-col gap-3 rounded-[8px] border border-dashed border-border-strong px-3.5 py-3 text-[12.5px] text-muted-foreground">
        <div className="flex items-center gap-1.5 text-[11px] tracking-[0.05em] text-faint uppercase">
          <Code className="size-3.5" />
          Developer
        </div>
        <div className="flex flex-wrap items-center gap-x-1.5 gap-y-1">
          runtime <IdText value={tool.runtime || "unknown"} />
          {tool.runtime === "host"
            ? " · runs inside substrate, held to the calling agent’s grants"
            : ` · stops after ${timeout ?? "5 seconds"}`}
        </div>
        {tool.description !== toolDescription(tool) && (
          <>
            <SubHead>What the model reads</SubHead>
            <p className="whitespace-pre-wrap text-foreground">
              {tool.description}
            </p>
          </>
        )}
        <SubHead>permissions</SubHead>
        <CodeBlock
          lang="yaml"
          source={permissionsYaml(tool)}
          className="rounded-[8px] border bg-background px-3 py-2"
        />
        {typeof source === "string" && source && (
          <>
            <SubHead>source</SubHead>
            <pre className="max-h-[480px] overflow-auto rounded-[8px] border bg-background px-3 py-2 font-mono text-[11.5px] leading-relaxed">
              {source}
            </pre>
          </>
        )}
        {tool.triggers.length > 0 && (
          <>
            <SubHead>triggers</SubHead>
            <ul className="flex flex-col gap-1">
              {tool.triggers.map((t) => (
                <li
                  key={t.id}
                  className="flex flex-wrap items-center gap-x-2 gap-y-0.5"
                >
                  <Link
                    to="/data/$authority/$pkg/$name/$id"
                    params={{
                      authority: "substrate.reamde.dev",
                      pkg: "core",
                      name: "trigger",
                      id: t.id,
                    }}
                    className="hover:underline"
                  >
                    <IdText
                      value={`substrate.reamde.dev/core/trigger/${t.id}`}
                    />
                  </Link>
                  {!t.enabled && <span className="text-faint">off</span>}
                  <TriggerProgress
                    status={statuses.data?.find((s) => s.id === t.id)}
                  />
                </li>
              ))}
            </ul>
          </>
        )}
        {tool.triggers.length > 0 && (
          <div className="flex flex-wrap items-center gap-x-1.5">
            each run is a record: <IdText value={TRIGGER_RUN_KIND} />
          </div>
        )}
        {tool.uses.length > 0 && (
          <div className="flex flex-wrap items-center gap-x-1.5">
            each agent call is a record:{" "}
            <IdText value={`${LLM_PACKAGE}/message`} /> with role tool
          </div>
        )}
      </div>
    </Section>
  )
}

function TriggerProgress({ status }: { status?: TriggerStatus }) {
  if (!status) return null
  return (
    <span
      data-slot="trigger-progress"
      className={cn(
        "tabular-nums",
        (status.lag ?? 0) > 0 || status.parked > 0 || status.error
          ? "text-warning"
          : "text-faint"
      )}
    >
      {triggerProgress(status).join(" · ")}
    </span>
  )
}

function SubHead({ children }: { children: ReactNode }) {
  return (
    <div className="-mb-1.5 text-[11.5px] font-medium text-faint">
      {children}
    </div>
  )
}
