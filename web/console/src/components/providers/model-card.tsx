/** One AI model card on Providers: an `llm/provider` row, titled by its
 * label, saying what it gives you, whether it works (On, Set up, Needs
 * attention; a row has no off switch, so never Paused), and whose key it
 * runs on. A key the vendor refused, for chat or for search by meaning
 * through the card, Needs attention and says why in the server's words. The
 * key form opens in place: **Use your own key** over the operator's,
 * **Replace key** over your own, **Add key** where there is none, and **Use
 * _operator_’s key again** where your key replaced one the operator still
 * offers. Until the operator's read answers, or where it failed, the card
 * says so in a neutral pill and never Set up: a row with no key of its own
 * may still be the operator's. The row's record page is a Technical details
 * link only.
 *
 * Under its key, a card lists the models the row offers (lib/llm-models.ts,
 * rule A): each with its price per 1M tokens, its window, and whether the
 * price is the row's own or its model record's. A row's own entry that wins
 * over a record pricing the model differently says so, and offers to drop
 * the entry so the record prices it. A row the operator serves lists the
 * operator's models, whose prices stay on the server.
 *
 * A seeded row the person deleted is an Add card, which writes the row back
 * from the seed's fields with no key: the seed is create-only and never
 * brings a deleted row back on its own. */

import { useEffect, useRef, useState } from "react"
import { useMutation, useQueryClient } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import { ChevronRightIcon, KeyRoundIcon } from "lucide-react"

import { IdText } from "@/components/identity/id-text"
import { Pill, type PillTone } from "@/components/identity/pill"
import {
  ModelKeyForm,
  type KeyFormMode,
} from "@/components/providers/model-key-form"
import { ProviderLogo } from "@/components/providers/provider-marks"
import { writeError } from "@/components/property-sheet/use-record-patch"
import { Button } from "@/components/ui/button"
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible"
import { ConfirmDialog } from "@/components/ui/confirm-dialog"
import { Spinner } from "@/components/ui/spinner"
import { providerInfo } from "@/lib/actor-identity"
import { CORE_AUTHORITY, LLM_PACKAGE_NAME } from "@/lib/api/http"
import { operatorDefaultsQueryOptions } from "@/lib/api/operator"
import { patchRecord, putRecord, recordWriteReaches } from "@/lib/api/records"
import type { SubstrateRecord } from "@/lib/api/types"
import {
  PRICE_SOURCE_WORDS,
  modelTitle,
  priceWords,
  pricingWithout,
  windowWords,
  type ModelOffer,
} from "@/lib/llm-models"
import { LLM_PROVIDER_SEED } from "@/lib/llm-provider-seed"
import {
  LLM_PROVIDER_KIND,
  cardState,
  modelName,
  modelWhoseKey,
  type ModelKey,
  type ModelState,
  type OperatorWords,
} from "@/lib/operator-defaults"
import { cn } from "@/lib/utils"

const STATE: Record<ModelState, { word: string; tone: PillTone }> = {
  on: { word: "On", tone: "ok" },
  setup: { word: "Set up", tone: "warn" },
  attention: { word: "Needs attention", tone: "bad" },
  checking: { word: "Checking", tone: "neutral" },
  unchecked: { word: "Couldn’t check", tone: "neutral" },
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
      <Button variant="ghost" size="sm" onClick={() => setOpen(true)}>
        Use {words.possessive} key again
      </Button>
      <ConfirmDialog
        open={open}
        title={`Use ${words.possessive} ${name} key again?`}
        consequence={`This deletes your ${name} key. Your agents then call ${name} with ${words.possessive} key, which counts toward today’s allowance. You can add your own key again at any time.`}
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

/** "Use the model record’s price": drops the row's own entries for one
 * model, so the record it shadows prices it from then on. */
function UseRecordPrice({
  row,
  name,
  offer,
}: {
  row: SubstrateRecord
  name: string
  offer: ModelOffer
}) {
  const [open, setOpen] = useState(false)
  const client = useQueryClient()
  const drop = useMutation({
    mutationFn: () =>
      patchRecord(CORE_AUTHORITY, LLM_PACKAGE_NAME, "provider", row.id, {
        properties: { pricing: pricingWithout(row, offer.model) },
        ifVersion: row.version,
      }),
    onSuccess: async () => {
      setOpen(false)
      await invalidates(client, row.id)
    },
  })
  const price = offer.record?.price
  if (!price) return null
  return (
    <>
      <Button
        variant="outline"
        size="xs"
        className="self-start"
        onClick={() => setOpen(true)}
      >
        Use the model record’s price
      </Button>
      <ConfirmDialog
        open={open}
        title={`Use the model record’s price for ${modelTitle({ label: offer.record?.label, model: offer.model })}?`}
        consequence={`This removes ${offer.model} from ${name}’s own price list. Its runs are then priced by its model record: ${priceWords(price)} per 1M tokens.`}
        confirm="Use the model record’s price"
        pending={drop.isPending}
        error={drop.error ? writeError(drop.error) : undefined}
        onConfirm={() => drop.mutate()}
        onClose={() => {
          drop.reset()
          setOpen(false)
        }}
      />
    </>
  )
}

const OFFER_FACTS =
  "grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-0.5 text-[12px]"

/** One model the row offers: its name and id, and, where the console can
 * see it, its price, its window and where the price comes from. */
function Offer({
  row,
  name,
  offer,
  operator,
}: {
  row: SubstrateRecord
  name: string
  offer: ModelOffer
  operator: boolean
}) {
  const label = offer.record?.label
  return (
    <li data-slot="model-offer" className="flex flex-col gap-1 py-2">
      <div className="flex flex-wrap items-baseline gap-x-2">
        <span className="font-medium">{label ?? offer.model}</span>
        {label && (
          <span className="data text-[11.5px] [overflow-wrap:anywhere] text-faint">
            {offer.model}
          </span>
        )}
      </div>
      {!operator && (
        <dl className={OFFER_FACTS}>
          <dt className="text-muted-foreground">Price per 1M tokens</dt>
          <dd className="min-w-0">
            {offer.price
              ? priceWords(offer.price)
              : "Not priced: a spend cap doesn’t count its runs, and an app’s approval doesn’t offer it"}
          </dd>
          <dt className="text-muted-foreground">Context window</dt>
          <dd className="min-w-0">
            {offer.window ? windowWords(offer.window) : "Not stated"}
          </dd>
          {(offer.source === "row" || offer.source === "record") && (
            <>
              <dt className="text-muted-foreground">Price from</dt>
              <dd className="min-w-0">{PRICE_SOURCE_WORDS[offer.source]}</dd>
            </>
          )}
        </dl>
      )}
      {offer.shadowsRecord && offer.record?.price && (
        <div className="flex flex-col gap-1.5 rounded-md bg-warn-soft px-2 py-1.5 text-[12px]">
          <p>
            Its model record says {priceWords(offer.record.price)}; this row’s
            own price list wins over it.
          </p>
          <UseRecordPrice row={row} name={name} offer={offer} />
        </div>
      )}
    </li>
  )
}

/** The models a row offers, folded under their count. */
function Offers({
  row,
  name,
  offers,
  operator,
  words,
}: {
  row: SubstrateRecord
  name: string
  offers: ModelOffer[]
  operator: boolean
  words: OperatorWords
}) {
  if (offers.length === 0) {
    return (
      <p className="text-[12.5px] text-muted-foreground">
        {operator
          ? `${words.Name} serves no model on it.`
          : "It offers no model yet: no model record names it."}
      </p>
    )
  }
  const count = offers.length === 1 ? "1 model" : `${offers.length} models`
  return (
    <Collapsible className="group/offers flex flex-col">
      <CollapsibleTrigger className="flex w-fit cursor-pointer items-center gap-1 text-[12.5px] font-medium text-muted-foreground hover:text-foreground">
        <ChevronRightIcon className="size-3.5 transition-transform group-data-open/offers:rotate-90" />
        {operator
          ? `${count} on ${words.possessive} key`
          : `${count} it offers`}
      </CollapsibleTrigger>
      <CollapsibleContent>
        <ul className="divide-y">
          {offers.map((offer) => (
            <Offer
              key={offer.model}
              row={row}
              name={name}
              offer={offer}
              operator={operator}
            />
          ))}
        </ul>
        {operator && (
          <p className="text-[12px] text-faint">
            {words.Name} sets what they cost.
          </p>
        )}
      </CollapsibleContent>
    </Collapsible>
  )
}

/** The key button's words and the form's mode, from where the key comes
 * from now. A source nobody can tell yet adds a key: what that says is true
 * whoever's key the row runs on. */
function keyAction(key: ModelKey): { label: string; mode: KeyFormMode } {
  if (key.source === "operator")
    return { label: "Use your own key", mode: "switch" }
  if (key.source === "own") return { label: "Replace key", mode: "replace" }
  return { label: "Add key", mode: "add" }
}

export function ModelCard({
  row,
  modelKey: key,
  offers,
  offersError,
  words,
  search,
  searchFailed,
  technical,
  focused,
}: {
  row: SubstrateRecord
  modelKey: ModelKey
  /** The models the row offers; undefined until they can be told. */
  offers?: ModelOffer[]
  /** Why the model records did not read. */
  offersError?: string
  words: OperatorWords
  /** "Search by meaning: Crane’s key", on the one card it runs through. */
  search?: string
  /** Why search by meaning through this card fails, in the server's words. */
  searchFailed?: string
  technical: boolean
  /** Addressed by `?model=`: scrolled to, and its key form opened where it
   * has no key. */
  focused?: boolean
}) {
  const name = modelName(row)
  const [editing, setEditing] = useState(
    Boolean(focused) && key.state === "setup"
  )
  // A card an agent's link names opens its form once it is known to have no
  // key; a card still checking learns that after it mounts.
  const [checked, setChecked] = useState(key.state !== "checking")
  if (!checked && key.state !== "checking") {
    setChecked(true)
    if (focused && key.state === "setup") setEditing(true)
  }
  const state = STATE[cardState(key, searchFailed)]
  const whose = modelWhoseKey(key, words)
  const action = keyAction(key)
  return (
    <CardShell rowId={row.id} focused={focused}>
      <div className="flex items-center gap-2.5">
        <Title rowId={row.id} name={name} />
        <Pill tone={state.tone}>{state.word}</Pill>
      </div>
      <p className="text-[12.5px] text-muted-foreground">
        Chat models for your agents.
      </p>
      {whose && <p className="text-[12.5px]">{whose}</p>}
      {key.status && (
        <p className="text-[12.5px] text-muted-foreground">{key.status}</p>
      )}
      {key.message && (
        <p
          className={cn(
            "text-[12.5px]",
            key.state === "attention" ? "text-destructive" : "text-warning"
          )}
        >
          {key.message}
        </p>
      )}
      {search && <p className="text-[12.5px]">{search}</p>}
      {searchFailed && searchFailed !== key.message && (
        <p className="text-[12.5px] text-destructive">{searchFailed}</p>
      )}
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
      {offersError ? (
        <p className="text-[12.5px] text-destructive">
          Couldn’t read the models it offers: {offersError}
        </p>
      ) : (
        offers && (
          <Offers
            row={row}
            name={name}
            offers={offers}
            operator={key.source === "operator"}
            words={words}
          />
        )
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
  return (
    <CardShell rowId={id}>
      <div className="flex items-center gap-2.5">
        <Title rowId={id} name={modelName({ id, properties: seed ?? {} })} />
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
        Chat models for your agents. This one was removed.{" "}
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
