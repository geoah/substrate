/** What an agent runs on, read and changed in one place, in one of the two
 * ways an owner's agent says it (a published agent's `providerInput` and
 * `models` are its bundle's choice):
 *
 * - `provider` + `model`: the llm/provider row its loop completes against
 *   and the model id it sends, read and changed together. A model id belongs
 *   to one provider's API (`gpt-5-mini` means nothing to Anthropic), so
 *   switching one without seeing the other leaves an agent that refuses at
 *   dispatch. The substrate does not check the pair, and neither does this:
 *   it keeps the two side by side and writes both in one PATCH under
 *   `ifVersion`. Switching to a list of models is one PATCH too: it sets
 *   `runsOn` and removes both, because the agent names exactly one of the
 *   ways and a write that left either behind would be refused.
 * - `runsOn`: llm/model records, best first (decision record 0242). Each
 *   entry shows its model, its provider, its price, and why a new chat would
 *   pass it over (lib/llm-models.ts). The list is edited as a draft and
 *   written whole, in one PATCH under `ifVersion`.
 *
 * The labels and descriptions are the agent kind's and the model kind's
 * own, as the record page shows them. */

import { useId, useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import {
  ArrowDownIcon,
  ArrowUpIcon,
  ListOrderedIcon,
  PencilIcon,
  XIcon,
} from "lucide-react"

import {
  useEditBase,
  useRecordPatch,
  writeError,
} from "@/components/property-sheet/use-record-patch"
import { EmptyValue } from "@/components/identity/empty-value"
import { Pill } from "@/components/identity/pill"
import { RecordRef } from "@/components/identity/record-ref"
import { RecordCombobox } from "@/components/record/record-combobox"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Spinner } from "@/components/ui/spinner"
import { useAgentRuns, type AgentRunsRead } from "@/hooks/use-agent-runs"
import { AGENT_KIND, LLM_MODEL_KIND } from "@/lib/core-kinds"
import { llmModelsQueryOptions } from "@/lib/api/agents"
import { LLM_PACKAGE } from "@/lib/api/http"
import { kindsQueryOptions } from "@/lib/api/kinds"
import type { KindInfo, SubstrateRecord } from "@/lib/api/types"
import { kindByIdentity } from "@/lib/definition"
import {
  RUNS_ON_MAX,
  agentProviderId,
  agentRunsOn,
  modelFacts,
  modelTitle,
  offerFor,
  priceWords,
  refusalWords,
  rowName,
  runsOnValue,
  type RunsOnEntry,
} from "@/lib/llm-models"
import { humanizeName, propSpecsByName } from "@/lib/record-schema"
import { recordTitleQueryOptions } from "@/lib/reference-titles"

const PROVIDER_KIND = `${LLM_PACKAGE}/provider`

const NO_KINDS: KindInfo[] = []

interface Words {
  label: string
  description?: string
}

/** A property's label and description as its kind declares them, so this
 * reads the way the record page's sheet does. */
function wordsFor(kinds: KindInfo[], identity: string, name: string): Words {
  const kind = kindByIdentity(kinds, identity)
  const spec = kind
    ? propSpecsByName(kind).find((s) => s.name === name)
    : undefined
  return {
    label: spec?.label ?? humanizeName(name),
    description: spec?.description,
  }
}

function modelOf(agent: SubstrateRecord): string {
  const model = agent.properties.model
  return typeof model === "string" ? model : ""
}

const ROW =
  "grid grid-cols-[minmax(96px,120px)_minmax(0,1fr)] gap-x-3 sm:grid-cols-[minmax(120px,170px)_minmax(0,1fr)]"

const FORM =
  "flex flex-col gap-3 rounded-lg border border-primary bg-background p-3 ring-3 ring-primary-soft"

export function RunsOn({ agent }: { agent: SubstrateRecord }) {
  return agentRunsOn(agent) ? (
    <ModelList agent={agent} />
  ) : (
    <ProviderAndModel agent={agent} />
  )
}

// ── provider + model ────────────────────────────────────────────────────────

function ProviderAndModel({ agent }: { agent: SubstrateRecord }) {
  const registry = useQuery(kindsQueryOptions)
  const kinds = registry.data ?? NO_KINDS
  const [editing, setEditing] = useState<"pair" | "list">()
  const provider = wordsFor(kinds, AGENT_KIND, "provider")
  const model = wordsFor(kinds, AGENT_KIND, "model")
  const runsOn = wordsFor(kinds, AGENT_KIND, "runsOn")

  if (editing === "pair") {
    return (
      <RunsOnForm
        agent={agent}
        kinds={kinds}
        provider={provider}
        model={model}
        onDone={() => setEditing(undefined)}
      />
    )
  }
  if (editing === "list") {
    return (
      <SwitchToList
        agent={agent}
        kinds={kinds}
        provider={provider}
        model={model}
        runsOn={runsOn}
        onDone={() => setEditing(undefined)}
      />
    )
  }

  const providerId = agentProviderId(agent)
  const modelId = modelOf(agent)
  return (
    <div className="border-t">
      <dl className={ROW}>
        <dt className="py-2.5 text-[13.5px] text-muted-foreground">
          {provider.label}
        </dt>
        <dd className="min-w-0 py-2.5 text-[13.5px]">
          {providerId ? (
            <RecordRef kind={PROVIDER_KIND} id={providerId} />
          ) : (
            <EmptyValue />
          )}
        </dd>
        <dt className="py-2.5 text-[13.5px] text-muted-foreground">
          {model.label}
        </dt>
        <dd className="min-w-0 py-2.5 text-[13.5px] [overflow-wrap:anywhere]">
          {modelId || <EmptyValue />}
        </dd>
      </dl>
      <div className="flex flex-wrap items-center gap-1">
        <Button
          size="xs"
          variant="ghost"
          className="text-muted-foreground"
          aria-label={`Change ${provider.label} and ${model.label}`}
          onClick={() => setEditing("pair")}
        >
          <PencilIcon aria-hidden />
          Change
        </Button>
        <Button
          size="xs"
          variant="ghost"
          className="text-muted-foreground"
          onClick={() => setEditing("list")}
        >
          <ListOrderedIcon aria-hidden />
          Switch to a list of models
        </Button>
      </div>
    </div>
  )
}

function FormError({ error }: { error?: string }) {
  if (!error) return null
  return (
    <p
      role="alert"
      className="rounded-md bg-bad-soft px-2.5 py-1.5 text-[12.5px] text-destructive"
    >
      {error}
    </p>
  )
}

function FormButtons({
  pending,
  save,
  onDone,
}: {
  pending: boolean
  save: string
  onDone: () => void
}) {
  return (
    <div className="flex items-center gap-2">
      <Button type="submit" size="sm" disabled={pending}>
        {pending && <Spinner className="size-3.5" />}
        {save}
      </Button>
      <Button type="button" size="sm" variant="ghost" onClick={onDone}>
        Cancel
      </Button>
    </div>
  )
}

function RunsOnForm({
  agent,
  kinds,
  provider,
  model,
  onDone,
}: {
  agent: SubstrateRecord
  kinds: KindInfo[]
  provider: Words
  model: Words
  onDone: () => void
}) {
  const uid = useId()
  const patch = useRecordPatch(agent, useEditBase(agent))
  const [initial] = useState(() => ({
    provider: agentProviderId(agent) ?? "",
    model: modelOf(agent),
  }))
  const [providerId, setProviderId] = useState(initial.provider)
  const [modelId, setModelId] = useState(initial.model)
  const [error, setError] = useState<string>()
  // The chosen row may sit past the loaded page; its title is read.
  const title = useQuery({
    ...recordTitleQueryOptions(PROVIDER_KIND, providerId),
    enabled: Boolean(providerId),
  })

  function save() {
    const typed = modelId.trim()
    // Both are required by the agent kind; the server refuses an empty one.
    if (!typed) {
      setError(`${model.label} is required.`)
      return
    }
    const next: Record<string, unknown> = {}
    if (providerId !== initial.provider) {
      next.provider = `${PROVIDER_KIND}/${providerId}`
    }
    if (typed !== initial.model) next.model = typed
    if (Object.keys(next).length === 0) {
      onDone()
      return
    }
    patch.mutate(next, {
      onSuccess: onDone,
      onError: (e) => setError(writeError(e)),
    })
  }

  return (
    <form
      className={FORM}
      onSubmit={(e) => {
        e.preventDefault()
        save()
      }}
    >
      <div className="flex flex-col gap-1">
        <label htmlFor={`${uid}-provider`} className="text-[13px] font-medium">
          {provider.label}
        </label>
        <RecordCombobox
          id={`${uid}-provider`}
          pin={PROVIDER_KIND}
          kinds={kinds}
          value={providerId}
          valueTitle={title.data ?? undefined}
          onSelect={setProviderId}
        />
        {provider.description && (
          <span className="text-xs text-muted-foreground">
            {provider.description}
          </span>
        )}
      </div>
      <div className="flex flex-col gap-1">
        <label htmlFor={`${uid}-model`} className="text-[13px] font-medium">
          {model.label}
        </label>
        <Input
          id={`${uid}-model`}
          className="data"
          spellCheck={false}
          autoComplete="off"
          value={modelId}
          onChange={(e) => setModelId(e.target.value)}
        />
        {model.description && (
          <span className="text-xs text-muted-foreground">
            {model.description}
          </span>
        )}
      </div>
      <FormError error={error} />
      <FormButtons pending={patch.isPending} save="Save" onDone={onDone} />
    </form>
  )
}

/** Moves an agent on `provider` + `model` to `runsOn`, starting with the
 * model record for the pair it runs on now where one exists. */
function SwitchToList({
  agent,
  kinds,
  provider,
  model,
  runsOn,
  onDone,
}: {
  agent: SubstrateRecord
  kinds: KindInfo[]
  provider: Words
  model: Words
  runsOn: Words
  onDone: () => void
}) {
  const uid = useId()
  const patch = useRecordPatch(agent, useEditBase(agent))
  const models = useQuery(llmModelsQueryOptions())
  const row = agentProviderId(agent)
  const current = models.data?.records
    .map(modelFacts)
    .find((f) => f.row === row && f.model === modelOf(agent))
  const [picked, setPicked] = useState<string>()
  const chosen = picked ?? current?.id ?? ""
  const [error, setError] = useState<string>()
  const title = useQuery({
    ...recordTitleQueryOptions(LLM_MODEL_KIND, chosen),
    enabled: Boolean(chosen),
  })

  function save() {
    if (!chosen) {
      setError("Choose the model it runs on first.")
      return
    }
    patch.mutate(
      { runsOn: runsOnValue([chosen]), provider: null, model: null },
      { onSuccess: onDone, onError: (e) => setError(writeError(e)) }
    )
  }

  return (
    <form
      className={FORM}
      onSubmit={(e) => {
        e.preventDefault()
        save()
      }}
    >
      <div className="flex flex-col gap-1">
        <label htmlFor={`${uid}-first`} className="text-[13px] font-medium">
          {runsOn.label}
        </label>
        <RecordCombobox
          id={`${uid}-first`}
          pin={LLM_MODEL_KIND}
          kinds={kinds}
          value={chosen}
          valueTitle={title.data ?? undefined}
          onSelect={setPicked}
        />
        {runsOn.description && (
          <span className="text-xs text-muted-foreground">
            {runsOn.description}
          </span>
        )}
      </div>
      <p className="text-[12.5px] text-muted-foreground">
        This replaces {provider.label} and {model.label} with a list that starts
        with this model. Add the others after.
      </p>
      <FormError error={error} />
      <FormButtons pending={patch.isPending} save="Switch" onDone={onDone} />
    </form>
  )
}

// ── runsOn ──────────────────────────────────────────────────────────────────

function ModelList({ agent }: { agent: SubstrateRecord }) {
  const registry = useQuery(kindsQueryOptions)
  const kinds = registry.data ?? NO_KINDS
  const read = useAgentRuns([agent])
  const [editing, setEditing] = useState(false)
  const runsOn = wordsFor(kinds, AGENT_KIND, "runsOn")

  if (editing) {
    return (
      <ModelListForm
        agent={agent}
        kinds={kinds}
        read={read}
        runsOn={runsOn}
        onDone={() => setEditing(false)}
      />
    )
  }

  const choice = read.of(agent).runsOn
  return (
    <div className="border-t">
      {choice && choice.entries.length > 0 ? (
        <ol className="divide-y">
          {choice.entries.map((entry, at) => (
            <EntryItem
              key={entry.id}
              entry={entry}
              position={at + 1}
              takes={entry === choice.chosen && entry.available === true}
              kinds={kinds}
              read={read}
            />
          ))}
        </ol>
      ) : (
        <p className="py-2.5 text-[13.5px]">
          <EmptyValue />
        </p>
      )}
      <Button
        size="xs"
        variant="ghost"
        className="text-muted-foreground"
        aria-label={`Change ${runsOn.label}`}
        onClick={() => setEditing(true)}
      >
        <PencilIcon aria-hidden />
        Change
      </Button>
    </div>
  )
}

const FACTS =
  "grid grid-cols-[minmax(88px,120px)_minmax(0,1fr)] gap-x-3 gap-y-1 text-[12.5px]"

/** One entry: its model, its provider, its price, and why a new chat would
 * pass it over. */
function EntryItem({
  entry,
  position,
  takes,
  kinds,
  read,
}: {
  entry: RunsOnEntry
  position: number
  /** A new chat runs on this one. */
  takes: boolean
  kinds: KindInfo[]
  read: AgentRunsRead
}) {
  const facts = entry.facts
  const model = wordsFor(kinds, LLM_MODEL_KIND, "model")
  const provider = wordsFor(kinds, LLM_MODEL_KIND, "provider")
  const pricing = wordsFor(kinds, LLM_MODEL_KIND, "pricing")
  const rows = read.keys.rows
  const offer = facts ? offerFor(facts.row, facts.model, read.ctx) : undefined
  return (
    <li className="grid grid-cols-[1.5rem_minmax(0,1fr)] gap-x-1 py-2.5 text-[13.5px]">
      <span className="text-faint tabular-nums">{position}.</span>
      <div className="flex min-w-0 flex-col gap-1.5">
        <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
          <span
            className={
              facts
                ? "font-medium"
                : "data font-medium [overflow-wrap:anywhere]"
            }
          >
            {facts ? modelTitle(facts) : entry.id}
          </span>
          {takes && <Pill tone="ok">New chats run on this</Pill>}
        </div>
        <dl className={FACTS}>
          {facts && (
            <>
              <dt className="text-muted-foreground">{model.label}</dt>
              <dd className="min-w-0 data [overflow-wrap:anywhere]">
                {facts.model || <EmptyValue />}
              </dd>
              <dt className="text-muted-foreground">{provider.label}</dt>
              <dd className="min-w-0">
                <Link
                  to="/providers"
                  search={{ model: facts.row }}
                  className="text-primary-text underline-offset-2 hover:underline"
                >
                  {rowName(rows, facts.row)}
                </Link>
              </dd>
              <dt className="text-muted-foreground">{pricing.label}</dt>
              <dd className="min-w-0">
                {!rows ? (
                  <EmptyValue />
                ) : offer?.source === "operator" ? (
                  `Set by ${read.keys.words.name}`
                ) : offer?.price ? (
                  `${priceWords(offer.price)} per 1M tokens`
                ) : (
                  "Not priced"
                )}
              </dd>
            </>
          )}
          {entry.refusal && (
            <>
              <dt className="text-muted-foreground">Can’t run now</dt>
              <dd className="min-w-0 text-warning">
                {refusalWords(entry.refusal, rows, read.keys.words)}
              </dd>
            </>
          )}
        </dl>
      </div>
    </li>
  )
}

function ModelListForm({
  agent,
  kinds,
  read,
  runsOn,
  onDone,
}: {
  agent: SubstrateRecord
  kinds: KindInfo[]
  read: AgentRunsRead
  runsOn: Words
  onDone: () => void
}) {
  const uid = useId()
  const patch = useRecordPatch(agent, useEditBase(agent))
  const [initial] = useState(() => agentRunsOn(agent) ?? [])
  const [ids, setIds] = useState(initial)
  const [error, setError] = useState<string>()
  const rows = read.keys.rows

  function move(from: number, to: number) {
    const next = [...ids]
    const [held] = next.splice(from, 1)
    next.splice(to, 0, held)
    setIds(next)
  }

  function save() {
    // The agent loader's bound: a list outside it refuses the write.
    if (ids.length === 0) {
      setError(`${runsOn.label} needs at least one model.`)
      return
    }
    if (
      ids.length === initial.length &&
      ids.every((id, at) => id === initial[at])
    ) {
      onDone()
      return
    }
    patch.mutate(
      { runsOn: runsOnValue(ids) },
      { onSuccess: onDone, onError: (e) => setError(writeError(e)) }
    )
  }

  return (
    <form
      className={FORM}
      onSubmit={(e) => {
        e.preventDefault()
        save()
      }}
    >
      <div className="flex flex-col gap-1">
        <span id={`${uid}-label`} className="text-[13px] font-medium">
          {runsOn.label}
        </span>
        {runsOn.description && (
          <span className="text-xs text-muted-foreground">
            {runsOn.description}
          </span>
        )}
      </div>
      {ids.length > 0 && (
        <ol aria-labelledby={`${uid}-label`} className="flex flex-col gap-1">
          {ids.map((id, at) => {
            const record = read.models?.find((r) => r.id === id)
            const facts = record ? modelFacts(record) : undefined
            const name = facts ? modelTitle(facts) : id
            return (
              <li
                key={id}
                className="flex items-center gap-1.5 rounded-md border px-2 py-1.5 text-[13px]"
              >
                <span className="w-5 shrink-0 text-faint tabular-nums">
                  {at + 1}.
                </span>
                <span className="flex min-w-0 flex-1 flex-col">
                  <span className={facts ? "truncate" : "truncate data"}>
                    {name}
                  </span>
                  <span className="truncate text-xs text-muted-foreground">
                    {facts
                      ? `${facts.model} on ${rowName(rows, facts.row)}`
                      : "Model record not found"}
                  </span>
                </span>
                <Button
                  type="button"
                  size="icon-xs"
                  variant="ghost"
                  aria-label={`Move ${name} up`}
                  disabled={at === 0}
                  onClick={() => move(at, at - 1)}
                >
                  <ArrowUpIcon />
                </Button>
                <Button
                  type="button"
                  size="icon-xs"
                  variant="ghost"
                  aria-label={`Move ${name} down`}
                  disabled={at === ids.length - 1}
                  onClick={() => move(at, at + 1)}
                >
                  <ArrowDownIcon />
                </Button>
                <Button
                  type="button"
                  size="icon-xs"
                  variant="ghost"
                  aria-label={`Remove ${name}`}
                  onClick={() => setIds(ids.filter((_, i) => i !== at))}
                >
                  <XIcon />
                </Button>
              </li>
            )
          })}
        </ol>
      )}
      {ids.length < RUNS_ON_MAX ? (
        <RecordCombobox
          pin={LLM_MODEL_KIND}
          kinds={kinds}
          adding
          addLabel="Add a model"
          exclude={new Set(ids)}
          onSelect={(id) => {
            setError(undefined)
            setIds([...ids, id])
          }}
        />
      ) : (
        <p className="text-xs text-muted-foreground">
          {runsOn.label} holds at most {RUNS_ON_MAX} models.
        </p>
      )}
      <FormError error={error} />
      <FormButtons pending={patch.isPending} save="Save" onDone={onDone} />
    </form>
  )
}
