/** A run's input and output as stored, for technical mode: a trigger run's
 * delivery and outcome, or an agent call's arguments (read off the assistant
 * turn that dispatched it, when opened) and its result. */

import { useQuery } from "@tanstack/react-query"

import { CodeBlock } from "@/components/code-block"
import { Skeleton } from "@/components/ui/skeleton"
import { callTurnQueryOptions } from "@/lib/api/functions"
import { callArguments, type ToolRun } from "@/lib/tools"

function json(value: unknown): string {
  return JSON.stringify(value, null, 2) ?? "null"
}

function Pane({
  title,
  children,
}: {
  title: string
  children: React.ReactNode
}) {
  return (
    <div className="min-w-0">
      <div className="mb-1 text-[11.5px] font-medium text-faint">{title}</div>
      {children}
    </div>
  )
}

function CallInput({ call }: { call: NonNullable<ToolRun["call"]> }) {
  const turn = useQuery(callTurnQueryOptions(call.thread, call.turn))
  if (turn.isPending) return <Skeleton className="h-16 w-full" />
  if (turn.error)
    return (
      <p className="text-[12.5px] text-muted-foreground">
        The call’s arguments didn’t load: {turn.error.message}
      </p>
    )
  const args = callArguments(turn.data, call.id)
  if (!args.found)
    return (
      <p className="text-[12.5px] text-muted-foreground">
        The turn that made this call is no longer stored.
      </p>
    )
  return <Block value={args.value} />
}

function Block({ value }: { value: unknown }) {
  return (
    <CodeBlock
      lang="json"
      source={json(value)}
      className="max-h-72 overflow-auto rounded-[8px] border bg-background px-3 py-2"
    />
  )
}

export function RunIO({ run }: { run: ToolRun }) {
  return (
    <div
      data-slot="run-io"
      className="grid gap-3 md:grid-cols-2"
      aria-label="Input and output"
    >
      <Pane title="Input">
        {run.call ? (
          <CallInput call={run.call} />
        ) : run.input && Object.keys(run.input).length ? (
          <Block value={run.input} />
        ) : (
          <p className="text-[12.5px] text-muted-foreground">Not stored.</p>
        )}
      </Pane>
      <Pane title="Output">
        {run.output && Object.keys(run.output).length ? (
          <Block value={run.output} />
        ) : (
          <p className="text-[12.5px] text-muted-foreground">Not stored.</p>
        )}
      </Pane>
    </div>
  )
}
