/** "Add tools" and "Add agents": the shipped samples that bring functions or
 * agents, each named by what it brings. Adding one imports the whole sample
 * under the repository's own authority, whatever collections come with it. */

import { useCallback } from "react"

import { AgentMark } from "@/components/agent/agent-mark"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { agentName } from "@/lib/actor-identity"
import type { BundleRow } from "@/lib/bundles"
import { toolName } from "@/lib/tools"
import { SampleList } from "./sample-list"
import { callableSamples } from "./sample-picks"

const WORDS = {
  functions: {
    title: "Add tools",
    description:
      "Samples that bring tools your agents can use. Adding one also adds anything it keeps its results in.",
    empty: "There are no sample tools to add.",
  },
  agents: {
    title: "Add agents",
    description:
      "Samples that bring agents, ready to chat with or to run on their own. Adding one also adds the tools and collections they work with.",
    empty: "There are no sample agents to add.",
  },
} as const

export function AddSampleDialog({
  plane,
  open,
  onOpenChange,
}: {
  plane: "functions" | "agents"
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const words = WORDS[plane]
  const pick = useCallback(
    (rows: BundleRow[]) => callableSamples(rows, plane),
    [plane]
  )
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>{words.title}</DialogTitle>
          <DialogDescription>{words.description}</DialogDescription>
        </DialogHeader>
        <SampleList
          pick={pick}
          empty={words.empty}
          member={(id) =>
            plane === "agents" ? (
              <>
                <AgentMark id={id} size="xs" />
                {agentName(id)}
              </>
            ) : (
              toolName(id)
            )
          }
        />
      </DialogContent>
    </Dialog>
  )
}
