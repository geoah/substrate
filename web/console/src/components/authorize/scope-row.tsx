/** One requested scope on the approval page: its checkbox and its everyday
 * sentence, and in the technical view the scope as written, what it reaches
 * by full reference and what an agent behind it may do. */

import { useId, useState } from "react"

import { Button } from "@/components/ui/button"
import type {
  AuthorizationKind,
  AuthorizationScope,
  SubstrateRecord,
} from "@/lib/api/types"
import { agentName, canChange, canSee } from "@/lib/agent-chat"
import { overAgents, scopeSentence } from "@/lib/app-scopes"

/** How many kinds a scope lists before it offers the rest. */
const KIND_PREVIEW = 8

function KindList({ kinds }: { kinds: AuthorizationKind[] }) {
  const [all, setAll] = useState(false)
  if (kinds.length === 0) {
    return (
      <p className="text-xs text-muted-foreground">
        Nothing yet: no kind in your repository matches.
      </p>
    )
  }
  const shown = all ? kinds : kinds.slice(0, KIND_PREVIEW)
  return (
    <div className="flex flex-col items-start gap-1">
      <ul className="flex w-full flex-col gap-1">
        {shown.map((k) => (
          <li
            key={k.identity}
            className="flex flex-wrap items-baseline gap-x-2 gap-y-0.5"
          >
            {k.label && <span className="text-[13px]">{k.label.plural}</span>}
            <span className="min-w-0 font-mono text-xs [overflow-wrap:anywhere] text-muted-foreground">
              {k.identity}
            </span>
            {k.added && (
              <span className="text-xs text-faint">added by this setup</span>
            )}
          </li>
        ))}
      </ul>
      {kinds.length > KIND_PREVIEW && (
        <Button
          variant="link"
          size="xs"
          className="px-0"
          onClick={() => setAll((v) => !v)}
        >
          {all ? "Show fewer" : `Show all ${kinds.length} kinds`}
        </Button>
      )}
    </div>
  )
}

/** The agents an agents or decide scope matches today. Talking to an agent
 * reaches whatever it may do, so each says what it can see and change in the
 * chat surface's words. */
function AgentList({
  agents,
  talk,
}: {
  agents: SubstrateRecord[]
  talk: boolean
}) {
  if (agents.length === 0) {
    return (
      <p className="text-xs text-muted-foreground">
        Nothing yet: no agent in your repository matches.
      </p>
    )
  }
  return (
    <ul className="flex w-full flex-col gap-2">
      {agents.map((a) => (
        <li key={a.id} className="flex flex-col gap-0.5">
          <div className="flex flex-wrap items-baseline gap-x-2 gap-y-0.5">
            <span className="text-[13px]">{agentName(a.id)}</span>
            <span className="min-w-0 font-mono text-xs [overflow-wrap:anywhere] text-muted-foreground">
              {a.id}
            </span>
          </div>
          {talk && (
            <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-0.5 text-xs">
              <dt className="text-muted-foreground">Can see</dt>
              <dd>{canSee(a) ?? "Nothing"}</dd>
              <dt className="text-muted-foreground">Can change</dt>
              <dd>{canChange(a)}</dd>
            </dl>
          )}
        </li>
      ))}
    </ul>
  )
}

/** What the technical view adds under a scope's sentence: the scope as the
 * token stores it, the `~` it was asked with, and everything it reaches. */
function ScopeFacts({
  scope,
  repository,
}: {
  scope: AuthorizationScope
  repository: string
}) {
  const agentsScope = overAgents(scope.verb)
  return (
    <>
      <p className="font-mono text-xs [overflow-wrap:anywhere] text-muted-foreground">
        {scope.scope}
      </p>
      {scope.glob && (
        <p className="text-xs text-muted-foreground">
          {agentsScope
            ? "Also covers matching agents you add later."
            : "Also covers matching kinds you add later."}
        </p>
      )}
      {scope.verb === "agents" && (
        <p className="text-xs text-muted-foreground">
          The app reads every chat with these agents, yours included, answers
          their questions, and may rename or stop a chat. A chat reaches
          whatever these agents may do. Its messages are the app’s; the agents
          work with their own permissions.
        </p>
      )}
      {scope.verb === "decide" && (
        <p className="text-xs text-muted-foreground">
          The app may edit a change before it applies it. A change the app
          accepts is applied as if you accepted it, and your history shows the
          decision as the app’s.
        </p>
      )}
      {scope.requested !== scope.scope && (
        <p className="text-xs text-muted-foreground">
          Asked for as{" "}
          <span className="font-mono [overflow-wrap:anywhere] text-foreground">
            {scope.requested}
          </span>
          , where <span className="font-mono text-foreground">~</span> means
          your repository,{" "}
          <span className="font-mono [overflow-wrap:anywhere] text-foreground">
            {repository}
          </span>
          .
        </p>
      )}
      <div className="flex flex-col gap-1">
        <p className="text-xs font-medium text-muted-foreground">
          Covers today
        </p>
        {agentsScope ? (
          <AgentList agents={scope.agents} talk={scope.verb === "agents"} />
        ) : (
          <KindList kinds={scope.kinds} />
        )}
      </div>
    </>
  )
}

export function ScopeRow({
  scope,
  repository,
  checked,
  disabled,
  technical,
  onChange,
}: {
  scope: AuthorizationScope
  repository: string
  checked: boolean
  disabled: boolean
  technical: boolean
  onChange: (checked: boolean) => void
}) {
  const id = useId()
  return (
    <li className="flex gap-3 px-3 py-3">
      <input
        id={id}
        type="checkbox"
        className="mt-0.5 size-4 shrink-0 cursor-pointer accent-primary disabled:cursor-default"
        checked={checked}
        disabled={disabled}
        onChange={(e) => onChange(e.target.checked)}
      />
      <div className="flex min-w-0 flex-1 flex-col gap-2">
        <label htmlFor={id} className="cursor-pointer">
          {scopeSentence(scope)}
        </label>
        {technical && <ScopeFacts scope={scope} repository={repository} />}
      </div>
    </li>
  )
}
