/** Where you write to the agent: a growing text box, the agent it goes to
 * (chosen on a new chat, fixed on an existing one, since a thread keeps its
 * agent) and the send button. Enter sends; Shift+Enter starts a new line. */

import { useEffect, useRef } from "react"
import { ArrowUpIcon, ChevronDownIcon } from "lucide-react"

import { AgentMark } from "@/components/agent/agent-mark"
import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Spinner } from "@/components/ui/spinner"
import { agentName } from "@/lib/agent-chat"

const MAX_HEIGHT = 200

export function Composer({
  value,
  onChange,
  onSend,
  agentId,
  agents,
  onPickAgent,
  busy,
  disabled,
}: {
  value: string
  onChange: (value: string) => void
  onSend: () => void
  agentId?: string
  /** The agents a new chat may go to; absent on an existing thread. */
  agents?: string[]
  onPickAgent?: (agent: string) => void
  /** A run is in flight. */
  busy: boolean
  /** Sending cannot work at all (no agent, no key). */
  disabled?: boolean
}) {
  const ref = useRef<HTMLTextAreaElement>(null)

  // Grows with what is written, up to a cap past which it scrolls.
  useEffect(() => {
    const el = ref.current
    if (!el) return
    el.style.height = "auto"
    el.style.height = `${Math.min(el.scrollHeight, MAX_HEIGHT)}px`
  }, [value])

  const name = agentId ? agentName(agentId) : "an agent"
  const canSend = !busy && !disabled && value.trim().length > 0

  return (
    <div className="px-4 pt-3 pb-4 md:px-6">
      <div className="flex flex-col gap-2 rounded-xl border border-border-strong bg-background px-3 py-2.5 focus-within:ring-2 focus-within:ring-ring/30">
        <textarea
          ref={ref}
          value={value}
          rows={1}
          onChange={(e) => onChange(e.target.value)}
          onKeyDown={(e) => {
            // A composition in progress (an IME) owns its own Enter.
            if (e.key !== "Enter" || e.shiftKey || e.nativeEvent.isComposing)
              return
            e.preventDefault()
            if (canSend) onSend()
          }}
          placeholder={`Ask ${name} anything about your data…`}
          aria-label={`Message ${name}`}
          disabled={disabled}
          className="min-h-[22px] resize-none bg-transparent text-sm leading-relaxed outline-none placeholder:text-faint disabled:cursor-not-allowed"
        />
        <div className="flex items-center gap-2 text-xs text-faint">
          {agents && onPickAgent ? (
            <DropdownMenu>
              <DropdownMenuTrigger
                render={
                  <button
                    type="button"
                    className="inline-flex cursor-pointer items-center gap-1.5 rounded-md border border-border-strong px-2 py-0.5 text-[12.5px] text-foreground hover:bg-hover"
                  />
                }
              >
                {agentId && <AgentMark id={agentId} size="xs" />}
                {agentId ? agentName(agentId) : "Choose an agent"}
                <ChevronDownIcon className="size-3.5 text-faint" />
              </DropdownMenuTrigger>
              <DropdownMenuContent align="start" side="top">
                {agents.map((id) => (
                  <DropdownMenuItem key={id} onClick={() => onPickAgent(id)}>
                    <AgentMark id={id} size="xs" />
                    {agentName(id)}
                  </DropdownMenuItem>
                ))}
              </DropdownMenuContent>
            </DropdownMenu>
          ) : agentId ? (
            <span className="inline-flex items-center gap-1.5">
              <AgentMark id={agentId} size="xs" />
              {agentName(agentId)}
            </span>
          ) : null}
          <span className="flex-1" />
          <span className="hidden sm:inline">
            Enter to send · Shift+Enter for a new line
          </span>
          <Button
            size="icon-sm"
            aria-label="Send"
            disabled={!canSend}
            onClick={onSend}
          >
            {busy ? <Spinner className="size-3.5" /> : <ArrowUpIcon />}
          </Button>
        </div>
      </div>
    </div>
  )
}
