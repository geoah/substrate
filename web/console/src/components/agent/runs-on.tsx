/** What an agent runs on: the llm/provider row its loop completes against and
 * the model id it sends, read and changed together. A model id belongs to one
 * provider's API (`gpt-5-mini` means nothing to Anthropic), so switching one
 * without seeing the other leaves an agent that refuses at dispatch. The
 * substrate does not check the pair, and neither does this: it keeps the two
 * side by side and writes both in one PATCH under `ifVersion`. The labels and
 * descriptions are the agent kind's own, as the record page shows them. */

import { useId, useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { PencilIcon } from "lucide-react"

import {
  useEditBase,
  useRecordPatch,
  writeError,
} from "@/components/property-sheet/use-record-patch"
import { EmptyValue } from "@/components/identity/empty-value"
import { RecordRef } from "@/components/identity/record-ref"
import { RecordCombobox } from "@/components/record/record-combobox"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Spinner } from "@/components/ui/spinner"
import { AGENT_KIND } from "@/lib/agent-grants"
import { agentProviderId } from "@/lib/agent-chat"
import { LLM_PACKAGE } from "@/lib/api/http"
import { kindsQueryOptions } from "@/lib/api/kinds"
import type { KindInfo, SubstrateRecord } from "@/lib/api/types"
import { kindByIdentity } from "@/lib/definition"
import { humanizeName, propSpecsByName } from "@/lib/record-schema"
import { recordTitleQueryOptions } from "@/lib/reference-titles"

const PROVIDER_KIND = `${LLM_PACKAGE}/provider`

const NO_KINDS: KindInfo[] = []

interface Words {
  label: string
  description?: string
}

/** A property's label and description as the agent kind declares them, so
 * this reads the way the record page's sheet does. */
function wordsFor(kinds: KindInfo[], name: string): Words {
  const kind = kindByIdentity(kinds, AGENT_KIND)
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

export function RunsOn({ agent }: { agent: SubstrateRecord }) {
  const registry = useQuery(kindsQueryOptions)
  const kinds = registry.data ?? NO_KINDS
  const [editing, setEditing] = useState(false)
  const provider = wordsFor(kinds, "provider")
  const model = wordsFor(kinds, "model")

  if (editing) {
    return (
      <RunsOnForm
        agent={agent}
        kinds={kinds}
        provider={provider}
        model={model}
        onDone={() => setEditing(false)}
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
      <Button
        size="xs"
        variant="ghost"
        className="text-muted-foreground"
        aria-label={`Change ${provider.label} and ${model.label}`}
        onClick={() => setEditing(true)}
      >
        <PencilIcon aria-hidden />
        Change
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
      className="flex flex-col gap-3 rounded-lg border border-primary bg-background p-3 ring-3 ring-primary-soft"
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
      {error && (
        <p
          role="alert"
          className="rounded-md bg-bad-soft px-2.5 py-1.5 text-[12.5px] text-destructive"
        >
          {error}
        </p>
      )}
      <div className="flex items-center gap-2">
        <Button type="submit" size="sm" disabled={patch.isPending}>
          {patch.isPending && <Spinner className="size-3.5" />}
          Save
        </Button>
        <Button type="button" size="sm" variant="ghost" onClick={onDone}>
          Cancel
        </Button>
      </div>
    </form>
  )
}
