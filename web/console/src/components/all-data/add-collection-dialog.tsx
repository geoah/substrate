/** "Add a collection": four ways to start one. Ask an agent to make it, add
 * a standard collection (a published package the catalog says adds
 * collections or metrics and connects to nothing, decision record 0238:
 * kept up to date for you, its fields not yours to change), start from a
 * sample (imported into the repository's own namespace `~`, the packages it
 * needs first; only samples that add a collection, see `collectionSamples`),
 * or declare the kind yourself in YAML. Asking goes to the agent that can
 * declare a kind, and is sent, not only written down. */

import { useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { Link, useNavigate } from "@tanstack/react-router"
import {
  BotIcon,
  CodeIcon,
  LayersIcon,
  LibraryIcon,
  type LucideIcon,
} from "lucide-react"

import { AgentRef } from "@/components/agent/agent-ref"
import { CopyButton } from "@/components/identity/copy-button"
import { KindGlyph } from "@/components/identity/kind-glyph"
import { SampleList } from "@/components/samples/sample-list"
import {
  collectionSamples,
  standardCollections,
} from "@/components/samples/sample-picks"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { radioKeys, radioTabIndex } from "@/components/ui/segmented"
import { Spinner } from "@/components/ui/spinner"
import { Textarea } from "@/components/ui/textarea"
import { lastChatAgent } from "@/lib/agent-chat"
import { collectionMaker } from "@/lib/agent-grants"
import { agentsQueryOptions, lastChatQueryOptions } from "@/lib/api/agents"
import type { KindInfo } from "@/lib/api/types"
import type { BundleRow } from "@/lib/bundles"
import { KIND_KIND } from "@/lib/core-kinds"
import { cn } from "@/lib/utils"
import { displayPlural } from "@/lib/kind-names"

// eslint-disable-next-line react-refresh/only-export-components -- the URL's word for each way, shared with the page that opens it
export const ADD_WAYS = ["agent", "standard", "sample", "yaml"] as const
type Way = (typeof ADD_WAYS)[number]

const WAYS: { value: Way; icon: LucideIcon; title: string; line: string }[] = [
  {
    value: "agent",
    icon: BotIcon,
    title: "Ask an agent",
    line: "Say what you want to keep. An agent sets it up.",
  },
  {
    value: "standard",
    icon: LibraryIcon,
    title: "Standard collections",
    line: "Health, lab results, your medical record: kept up to date for you.",
  },
  {
    value: "sample",
    icon: LayersIcon,
    title: "Start from a sample",
    line: "Tasks, people, calendars: ready-made collections you can change.",
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
          className="grid gap-2 sm:grid-cols-2"
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
          {way === "agent" && <AskAnAgent onSample={() => setWay("sample")} />}
          {way === "standard" && <Standard />}
          {way === "sample" && <Samples />}
          {way === "yaml" && <WriteIt />}
        </div>
      </DialogContent>
    </Dialog>
  )
}

/** Ask the one agent that can set a collection up, and send the request
 * rather than only writing it down: the Agents page opens a new chat with it
 * and asks at once. An agent can when its write grant covers the kind kind
 * and it holds the `write` tool (`canDeclareKinds`); the one you last
 * chatted with is asked when it can. When none can, the dialog says so
 * instead of handing the request to one that would refuse. */
function AskAnAgent({ onSample }: { onSample: () => void }) {
  const navigate = useNavigate()
  const [text, setText] = useState("")
  const agents = useQuery(agentsQueryOptions())
  // Only a preference: when this read fails, any agent that can is asked.
  const lastChat = useQuery(lastChatQueryOptions())
  const maker = collectionMaker(
    agents.data?.records ?? [],
    lastChatAgent(lastChat.data?.records ?? [])
  )

  if (agents.isPending || lastChat.isPending) {
    return (
      <p className="flex items-center gap-1.5 text-muted-foreground">
        <Spinner className="size-3" /> Loading your agents
      </p>
    )
  }
  // A failed read is not an answer: saying none can would be a guess.
  if (agents.isError) {
    return (
      <p role="alert" className="text-destructive">
        Your agents didn’t load: {agents.error.message}
      </p>
    )
  }
  if (!maker) {
    return (
      <div className="flex flex-col items-start gap-3">
        <p>
          None of your agents can set up a collection yet. Start from a sample,
          or give an agent that permission.
        </p>
        <div className="flex flex-wrap gap-2">
          <Button onClick={onSample}>Start from a sample</Button>
          <Button
            variant="outline"
            render={<Link to="/agents" />}
            nativeButton={false}
          >
            Go to Agents
          </Button>
        </div>
      </div>
    )
  }
  const ask = () => {
    const prompt = text.trim()
    if (!prompt) return
    // Leaving the page closes the dialog. Closing it first writes the page's
    // own address (`?add=` dropped) after this navigation and lands back here.
    const q = new URLSearchParams({ agent: maker.id, prompt, send: "1" })
    void navigate({ href: `/agents?${q.toString()}` })
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
      <div className="flex flex-wrap items-center gap-3">
        <Button type="submit" disabled={!text.trim()}>
          Ask
        </Button>
        <span className="flex flex-wrap items-center gap-1.5 text-[12.5px] text-muted-foreground">
          <AgentRef id={maker.id} agent={maker} /> sets it up, in a new chat.
        </span>
      </div>
    </form>
  )
}

function pickCollections(rows: BundleRow[], registry: KindInfo[]) {
  return collectionSamples(rows, registry)
}

function pickStandard(rows: BundleRow[], registry: KindInfo[]) {
  return standardCollections(rows, registry)
}

/** One collection a package adds, with its glyph. */
function collectionMember(k: string, registry: readonly KindInfo[]) {
  const held = registry.find((entry) => entry.identity === k) ?? k
  return (
    <>
      <KindGlyph kind={held} size="xs" />
      {displayPlural(held)}
    </>
  )
}

/** A standard collection by the collections that will show up. */
function Standard() {
  return (
    <div className="flex flex-col gap-2.5">
      <p className="text-[12.5px] text-muted-foreground">
        One shape every app and provider writes, kept up to date for you. You
        add the records; to change the fields, start from a sample instead.
      </p>
      <SampleList
        pick={pickStandard}
        empty="There are no standard collections to add."
        member={collectionMember}
      />
    </div>
  )
}

/** A sample by the collections that will show up, each with its glyph. */
function Samples() {
  return (
    <SampleList
      pick={pickCollections}
      empty="There are no sample collections to add."
      member={collectionMember}
    />
  )
}

function WriteIt() {
  const apply = "substratectl apply -f collection.yaml"
  // The CLI refuses a bare `kind` (record 0101): the kind kind is named in full.
  const example = `substratectl get ${KIND_KIND} <reference> -o yaml`
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
