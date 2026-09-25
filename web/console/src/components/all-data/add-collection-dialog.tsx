/** "Add a collection": three ways to start one. Ask an agent to make it,
 * start from a sample (imported under the repository's own authority, the
 * packages it needs first), or declare the kind yourself in YAML. */

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
import { TakeButton } from "@/components/providers/bundle-actions"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
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
  upgradeAvailable,
  upgradeBlocked,
  type BundleRow,
  type RequirementNode,
} from "@/lib/bundles"
import { cn } from "@/lib/utils"
import { packageDisplayName } from "@/lib/kind-names"

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
    line: "Tasks, people, notes: ready-made collections to copy.",
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
        >
          {WAYS.map((w) => (
            <button
              key={w.value}
              type="button"
              role="radio"
              aria-checked={way === w.value}
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

type SampleState = "add" | "upgrade" | "added"

function sampleState(row: BundleRow): SampleState {
  if (!row.installed) return "add"
  if (upgradeAvailable(row) && !upgradeBlocked(row)) return "upgrade"
  return "added"
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
  const samples = rows
    .filter((r) => r.tier === "sample")
    .sort((a, b) => a.name.localeCompare(b.name))

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
      <p className="text-muted-foreground">This substrate ships no samples.</p>
    )
  }
  return (
    <ul className="max-h-80 overflow-y-auto rounded-[10px] border border-border">
      {samples.map((row) => (
        <SampleRow key={row.id} row={row} chain={chains.get(row.id) ?? []} />
      ))}
    </ul>
  )
}

function SampleRow({
  row,
  chain,
}: {
  row: BundleRow
  chain: RequirementNode[]
}) {
  const state = sampleState(row)
  const missing = missingChain(chain)
  const name = packageDisplayName(row.name)
  return (
    <li className="border-b border-border px-3.5 py-3 last:border-b-0">
      <div className="flex items-start gap-3">
        <div className="min-w-0 flex-1">
          <div className="font-medium">{name}</div>
          {row.catalog?.description && (
            <p className="mt-0.5 text-[12.5px] text-faint">
              {row.catalog.description}
            </p>
          )}
          {state === "add" && missing.length > 0 && (
            <p className="mt-1 text-[12.5px] text-muted-foreground">
              Adds{" "}
              {missing
                .map((m) => packageDisplayName(m.row?.name ?? m.package))
                .join(", ")}{" "}
              first, which it needs.
            </p>
          )}
        </div>
        {state === "added" ? (
          <span className="inline-flex shrink-0 items-center gap-1 pt-0.5 text-[12.5px] text-ok">
            <CheckIcon className="size-3.5" />
            Added
          </span>
        ) : (
          // The providers' own door: the whole missing chain leaves first,
          // and a copy that would be replaced is confirmed before it is.
          <TakeButton
            row={row}
            chain={chain}
            name={name}
            label={state === "upgrade" ? "Upgrade" : "Add"}
            variant={state === "add" ? "default" : "outline"}
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
