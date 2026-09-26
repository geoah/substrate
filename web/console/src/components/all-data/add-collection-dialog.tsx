/** "Add a collection": three ways to start one. Ask an agent to make it,
 * start from a sample (imported under the repository's own authority, the
 * packages it needs first; only samples that add a collection, see
 * `collectionSamples`), or declare the kind yourself in YAML. */

import { useMemo, useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { useNavigate } from "@tanstack/react-router"
import {
  BotIcon,
  CheckIcon,
  CodeIcon,
  LayersIcon,
  type LucideIcon,
} from "lucide-react"

import { CopyButton } from "@/components/identity/copy-button"
import { KindGlyph } from "@/components/identity/kind-glyph"
import { TakeButton } from "@/components/providers/bundle-actions"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { radioKeys, radioTabIndex } from "@/components/ui/segmented"
import { Skeleton } from "@/components/ui/skeleton"
import { Textarea } from "@/components/ui/textarea"
import { bundleStatusesQueryOptions } from "@/lib/api/bundles"
import { catalogQueryOptions } from "@/lib/api/catalog"
import { kindsQueryOptions } from "@/lib/api/kinds"
import { repositoryQueryOptions } from "@/lib/api/repository"
import { getRepository } from "@/lib/api/session"
import {
  heldVersions,
  mergeBundles,
  missingChain,
  presentPackages,
  requirementTree,
  type RequirementNode,
} from "@/lib/bundles"
import { joinWords } from "@/lib/agent-chat"
import type { KindInfo } from "@/lib/api/types"
import { cn } from "@/lib/utils"
import { displayPlural, packageDisplayName } from "@/lib/kind-names"
import { collectionSamples, type SampleCollection } from "./sample-collections"

// eslint-disable-next-line react-refresh/only-export-components -- the URL's word for each way, shared with the page that opens it
export const ADD_WAYS = ["agent", "sample", "yaml"] as const
type Way = (typeof ADD_WAYS)[number]

const WAYS: { value: Way; icon: LucideIcon; title: string; line: string }[] = [
  {
    value: "agent",
    icon: BotIcon,
    title: "Ask an agent",
    line: "Say what you want to keep. An agent sets it up.",
  },
  {
    value: "sample",
    icon: LayersIcon,
    title: "Start from a sample",
    line: "Tasks, people, calendars: ready-made collections to add.",
  },
  {
    value: "yaml",
    icon: CodeIcon,
    title: "Write it yourself",
    line: "For developers: declare the kind in YAML.",
  },
]

/** Open while `way` is set; the way is the caller's (the page keeps it in
 * the URL, so a link can open the dialog on samples). */
export function AddCollectionDialog({
  way,
  onWayChange,
}: {
  way: Way | null
  onWayChange: (way: Way | null) => void
}) {
  const setWay = (next: Way) => onWayChange(next)
  const onOpenChange = (open: boolean) => {
    if (!open) onWayChange(null)
  }
  return (
    <Dialog open={way !== null} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>Add a collection</DialogTitle>
          <DialogDescription>
            A collection holds one kind of record: tasks, recipes, trips. Pick
            how to start it.
          </DialogDescription>
        </DialogHeader>
        <div
          role="radiogroup"
          aria-label="How to start"
          className="grid gap-2 sm:grid-cols-3"
          onKeyDown={radioKeys<Way | null>(
            ADD_WAYS,
            way,
            (w) => w && setWay(w)
          )}
        >
          {WAYS.map((w) => (
            <button
              key={w.value}
              type="button"
              role="radio"
              aria-checked={way === w.value}
              tabIndex={radioTabIndex<Way | null>(ADD_WAYS, way, w.value)}
              onClick={() => setWay(w.value)}
              className={cn(
                "flex cursor-pointer flex-col items-start gap-1.5 rounded-[10px] border border-border-strong bg-background p-3 text-left transition-shadow outline-none focus-visible:border-ring",
                way === w.value &&
                  "border-primary shadow-[0_0_0_3px_var(--primary-soft)]"
              )}
            >
              <w.icon className="size-4 text-muted-foreground" />
              <span className="font-semibold">{w.title}</span>
              <span className="text-[12.5px] text-faint">{w.line}</span>
            </button>
          ))}
        </div>
        <div className="min-h-40">
          {way === "agent" && <AskAnAgent onDone={() => onOpenChange(false)} />}
          {way === "sample" && <Samples />}
          {way === "yaml" && <WriteIt />}
        </div>
      </DialogContent>
    </Dialog>
  )
}

function AskAnAgent({ onDone }: { onDone: () => void }) {
  const navigate = useNavigate()
  const [text, setText] = useState("")
  const ask = () => {
    onDone()
    const prompt = text.trim()
    void navigate({
      href: prompt ? `/agents?prompt=${encodeURIComponent(prompt)}` : "/agents",
    })
  }
  return (
    <form
      className="flex flex-col gap-2.5"
      onSubmit={(e) => {
        e.preventDefault()
        ask()
      }}
    >
      <label htmlFor="collection-ask" className="font-medium">
        What do you want to keep track of?
      </label>
      <Textarea
        id="collection-ask"
        rows={3}
        placeholder="A recipe book, with ingredients, cooking time and the cuisine"
        value={text}
        onChange={(e) => setText(e.target.value)}
      />
      <div className="flex items-center gap-3">
        <Button type="submit">Ask</Button>
        <span className="text-[12.5px] text-faint">
          Opens Agents with this as your message.
        </span>
      </div>
    </form>
  )
}

function Samples() {
  const statuses = useQuery(bundleStatusesQueryOptions)
  const catalog = useQuery(catalogQueryOptions)
  const repository = useQuery(repositoryQueryOptions)
  const registry = useQuery(kindsQueryOptions)
  const home = repository.data?.authority ?? getRepository() ?? ""

  const rows = useMemo(
    () => mergeBundles(statuses.data ?? [], catalog.data ?? [], home),
    [statuses.data, catalog.data, home]
  )
  const chains = useMemo(() => {
    const present = presentPackages(rows, registry.data ?? [])
    const versions = heldVersions(rows)
    const byId = new Map(rows.map((row) => [row.id, row]))
    return new Map(
      rows.map((row) => [row.id, requirementTree(row, byId, present, versions)])
    )
  }, [rows, registry.data])
  const samples = useMemo(
    () => collectionSamples(rows, registry.data ?? []),
    [rows, registry.data]
  )

  if (catalog.isPending || statuses.isPending || registry.isPending) {
    return (
      <div className="flex flex-col gap-2">
        {Array.from({ length: 4 }, (_, i) => (
          <Skeleton key={i} className="h-12 w-full" />
        ))}
      </div>
    )
  }
  if (catalog.isError) {
    return (
      <p className="text-muted-foreground">
        The samples didn’t load: {catalog.error.message}
      </p>
    )
  }
  if (!samples.length) {
    return (
      <p className="text-muted-foreground">
        There are no sample collections to add.
      </p>
    )
  }
  return (
    <ul className="max-h-80 overflow-y-auto rounded-[10px] border border-border">
      {samples.map((sample) => (
        <SampleRow
          key={sample.row.id}
          sample={sample}
          registry={registry.data ?? []}
          chain={chains.get(sample.row.id) ?? []}
        />
      ))}
    </ul>
  )
}

/** A sample by what it adds: the collections that will show up, each with
 * its glyph. An added one says so; updating it is the package page's job. */
function SampleRow({
  sample: { row, kinds },
  registry,
  chain,
}: {
  sample: SampleCollection
  registry: readonly KindInfo[]
  chain: RequirementNode[]
}) {
  const missing = missingChain(chain)
  const name = packageDisplayName(row.name)
  const shown = kinds.map(
    (k) => registry.find((entry) => entry.identity === k) ?? k
  )
  return (
    <li className="border-b border-border px-3.5 py-3 last:border-b-0">
      <div className="flex items-start gap-3">
        <div className="min-w-0 flex-1">
          <div className="font-medium">{name}</div>
          <p className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-[12.5px] text-muted-foreground">
            {shown.map((k) => (
              <span
                key={typeof k === "string" ? k : k.identity}
                className="inline-flex items-center gap-1.5"
              >
                <KindGlyph kind={k} size="xs" />
                {displayPlural(k)}
              </span>
            ))}
          </p>
          {!row.installed && missing.length > 0 && (
            <p className="mt-1 text-[12.5px] text-faint">
              Adds{" "}
              {joinWords(
                missing.map((m) => packageDisplayName(m.row?.name ?? m.package))
              )}{" "}
              first, which it needs.
            </p>
          )}
        </div>
        {row.installed ? (
          <span className="inline-flex shrink-0 items-center gap-1 pt-0.5 text-[12.5px] text-ok">
            <CheckIcon className="size-3.5" />
            Added
          </span>
        ) : (
          // The providers' own door: the whole missing chain lands first.
          <TakeButton
            row={row}
            chain={chain}
            name={name}
            label="Add"
            variant="default"
          />
        )}
      </div>
    </li>
  )
}

function WriteIt() {
  const apply = "substratectl apply -f collection.yaml"
  const example = "substratectl get kind <reference> -o yaml"
  return (
    <div className="flex flex-col gap-3 text-muted-foreground">
      <p>
        A collection is a kind: a YAML document naming its properties. Write one
        and apply it from the command line; it shows up here as soon as it
        lands.
      </p>
      {[example, apply].map((command) => (
        <div
          key={command}
          className="flex items-center gap-2 rounded-lg border border-border bg-panel px-3 py-2"
        >
          <code className="min-w-0 flex-1 font-mono text-[12.5px] [overflow-wrap:anywhere] text-foreground">
            {command}
          </code>
          <CopyButton value={command} label={`Copy ${command}`} />
        </div>
      ))}
      <p className="text-[12.5px] text-faint">
        The first command prints an existing kind to start from; the second
        applies yours.
      </p>
    </div>
  )
}
