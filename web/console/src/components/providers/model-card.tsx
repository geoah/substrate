/** One AI model card on Providers: an `llm/provider` row, titled by its
 * label, saying what it gives you, whether it works (On, Set up, Needs
 * attention; a row has no off switch, so never Paused), and whose key it
 * runs on. The key form opens in place: **Use your own key** over the
 * operator's, **Replace key** over your own, **Add key** where there is
 * none, and **Use _operator_’s again** where your key replaced one the
 * operator still offers. The row's record page is a Technical details
 * link only.
 *
 * A seeded row the person deleted is an Add card, which writes the row back
 * from the seed's fields with no key: the seed is create-only and never
 * brings a deleted row back on its own. */

import { useEffect, useRef, useState } from "react"
import { useMutation, useQueryClient } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import { KeyRoundIcon } from "lucide-react"

import { IdText } from "@/components/identity/id-text"
import { Pill, type PillTone } from "@/components/identity/pill"
import {
  ModelKeyForm,
  type KeyFormMode,
} from "@/components/providers/model-key-form"
import { ProviderLogo } from "@/components/providers/provider-marks"
import { writeError } from "@/components/property-sheet/use-record-patch"
import { Button } from "@/components/ui/button"
import { ConfirmDialog } from "@/components/ui/confirm-dialog"
import { Spinner } from "@/components/ui/spinner"
import { providerInfo } from "@/lib/actor-identity"
import { providerName } from "@/lib/agent-chat"
import { CORE_AUTHORITY, LLM_PACKAGE_NAME } from "@/lib/api/http"
import { operatorDefaultsQueryOptions } from "@/lib/api/operator"
import { patchRecord, putRecord, recordWriteReaches } from "@/lib/api/records"
import type { SubstrateRecord } from "@/lib/api/types"
import { LLM_PROVIDER_SEED } from "@/lib/llm-provider-seed"
import {
  LLM_PROVIDER_KIND,
  modelName,
  modelWhoseKey,
  type ModelKey,
  type ModelState,
  type OperatorWords,
} from "@/lib/operator-defaults"
import { andList } from "@/lib/providers"
import { cn } from "@/lib/utils"

const STATE: Record<ModelState, { word: string; tone: PillTone }> = {
  on: { word: "On", tone: "ok" },
  setup: { word: "Set up", tone: "warn" },
  attention: { word: "Needs attention", tone: "bad" },
}

/** The element id a card is scrolled to by: `?model=<row id>`. */
function modelCardId(rowId: string): string {
  return `model-${rowId}`
}

function invalidates(client: ReturnType<typeof useQueryClient>, id: string) {
  return client.invalidateQueries({
    predicate: (q) =>
      recordWriteReaches(q.queryKey, LLM_PROVIDER_KIND, id) ||
      q.queryKey[0] === operatorDefaultsQueryOptions.queryKey[0],
  })
}

function CardShell({
  rowId,
  focused,
  children,
}: {
  rowId: string
  focused?: boolean
  children: React.ReactNode
}) {
  const ref = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (focused) ref.current?.scrollIntoView?.({ block: "center" })
  }, [focused])
  return (
    <div
      ref={ref}
      id={modelCardId(rowId)}
      data-slot="model-card"
      data-focused={focused || undefined}
      className={cn(
        "flex scroll-mt-6 flex-col gap-2 rounded-[10px] border bg-background p-3.5",
        focused && "border-primary ring-2 ring-primary/25"
      )}
    >
      {children}
    </div>
  )
}

function Title({ rowId, name }: { rowId: string; name: string }) {
  return (
    <>
      <ProviderLogo info={providerInfo(rowId)} />
      <h3 className="min-w-0 flex-1 truncate font-semibold">{name}</h3>
    </>
  )
}

/** The record page of one row: technical mode only. */
function RecordLink({ id }: { id: string }) {
  return (
    <Link
      to="/data/$authority/$pkg/$name/$id"
      params={{
        authority: CORE_AUTHORITY,
        pkg: LLM_PACKAGE_NAME,
        name: "provider",
        id,
      }}
      className="text-[12.5px] text-primary-text underline-offset-2 hover:underline"
    >
      Open record
    </Link>
  )
}

function UseOperatorAgain({
  row,
  name,
  words,
}: {
  row: SubstrateRecord
  name: string
  words: OperatorWords
}) {
  const [open, setOpen] = useState(false)
  const client = useQueryClient()
  const clear = useMutation({
    mutationFn: () =>
      patchRecord(CORE_AUTHORITY, LLM_PACKAGE_NAME, "provider", row.id, {
        properties: { apiKey: null },
        ifVersion: row.version,
      }),
    onSuccess: async () => {
      setOpen(false)
      await invalidates(client, row.id)
    },
  })
  return (
    <>
      <Button variant="outline" size="sm" onClick={() => setOpen(true)}>
        Use {words.possessive} again
      </Button>
      <ConfirmDialog
        open={open}
        title={`Use ${words.possessive} ${name} key again?`}
        consequence={`Your ${name} key is deleted, and your agents call ${name} with ${words.possessive} key, within its daily allowance. You can add your own key again at any time.`}
        confirm={`Use ${words.possessive} key`}
        destructive
        pending={clear.isPending}
        error={clear.error ? writeError(clear.error) : undefined}
        onConfirm={() => clear.mutate()}
        onClose={() => {
          clear.reset()
          setOpen(false)
        }}
      />
    </>
  )
}

/** The key button's words and the form's mode, from where the key comes
 * from now. */
function keyAction(key: ModelKey): { label: string; mode: KeyFormMode } {
  if (key.source === "operator")
    return { label: "Use your own key", mode: "switch" }
  if (key.source === "own") return { label: "Replace key", mode: "replace" }
  return { label: "Add key", mode: "add" }
}

export function ModelCard({
  row,
  modelKey: key,
  words,
  search,
  technical,
  focused,
}: {
  row: SubstrateRecord
  modelKey: ModelKey
  words: OperatorWords
  /** "Search by meaning: Crane’s key", on the one card it runs through. */
  search?: string
  technical: boolean
  /** Addressed by `?model=`: scrolled to, and its key form opened where it
   * has no key. */
  focused?: boolean
}) {
  const name = modelName(row)
  const [editing, setEditing] = useState(
    Boolean(focused) && key.state === "setup"
  )
  const state = STATE[key.state]
  const whose = modelWhoseKey(key, words)
  const action = keyAction(key)
  const models = key.models?.length ? andList(key.models) : undefined
  return (
    <CardShell rowId={row.id} focused={focused}>
      <div className="flex items-center gap-2.5">
        <Title rowId={row.id} name={name} />
        <Pill tone={state.tone}>{state.word}</Pill>
      </div>
      <p className="text-[12.5px] text-muted-foreground">
        Chat models for your agents
        {models ? `: ${models}` : ""}.
      </p>
      {whose && <p className="text-[12.5px]">{whose}</p>}
      {key.message && (
        <p className="text-[12.5px] text-destructive">{key.message}</p>
      )}
      {search && <p className="text-[12.5px]">{search}</p>}
      {editing ? (
        <ModelKeyForm
          row={row}
          name={name}
          words={words}
          mode={action.mode}
          onDone={() => setEditing(false)}
        />
      ) : (
        <div className="mt-auto flex flex-wrap items-center gap-2 pt-1">
          <Button
            variant={key.source === "none" ? "default" : "outline"}
            size="sm"
            onClick={() => setEditing(true)}
          >
            <KeyRoundIcon /> {action.label}
          </Button>
          {key.source === "own" && key.operatorOffers && (
            <UseOperatorAgain row={row} name={name} words={words} />
          )}
        </div>
      )}
      {technical && (
        <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
          <IdText value={`${LLM_PROVIDER_KIND}/${row.id}`} />
          <RecordLink id={row.id} />
        </div>
      )}
    </CardShell>
  )
}

/** A seeded row the person deleted: Add writes it back with the seed's
 * fields and no key. */
export function AddModelCard({
  id,
  words,
  operatorOffers,
  technical,
}: {
  /** The seeded row's id: `openai`, `anthropic`, `gemini`. */
  id: string
  words: OperatorWords
  /** The operator serves this row, so its key covers it once it is back. */
  operatorOffers: boolean
  technical: boolean
}) {
  const client = useQueryClient()
  const seed = LLM_PROVIDER_SEED[id]
  const add = useMutation({
    mutationFn: () =>
      putRecord(CORE_AUTHORITY, LLM_PACKAGE_NAME, "provider", id, {
        properties: { ...seed },
      }),
    onSuccess: () => invalidates(client, id),
  })
  const label = typeof seed?.label === "string" ? seed.label : id
  return (
    <CardShell rowId={id}>
      <div className="flex items-center gap-2.5">
        <Title rowId={id} name={providerName(label)} />
        <Button
          size="sm"
          className="h-6"
          disabled={add.isPending}
          onClick={() => add.mutate()}
        >
          {add.isPending && <Spinner className="size-3" />}
          Add
        </Button>
      </div>
      <p className="text-[12.5px] text-muted-foreground">
        Chat models for your agents. You removed it.{" "}
        {operatorOffers
          ? `Add it back and ${words.possessive} key covers it again.`
          : "Add it back, then add a key."}
      </p>
      {add.error && (
        <p role="alert" className="text-[12.5px] text-destructive">
          {writeError(add.error)}
        </p>
      )}
      {technical && <IdText value={`${LLM_PROVIDER_KIND}/${id}`} />}
    </CardShell>
  )
}
