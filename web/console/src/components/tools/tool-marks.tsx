/** The Tools pages' small marks: a tool's icon tile, its status pill, and
 * where it comes from. */

import type { ReactNode } from "react"
import {
  AtSign,
  Calendar,
  FileText,
  HardDrive,
  Hash,
  Mail,
  MessageCircleQuestion,
  Pencil,
  RefreshCw,
  Search,
  ShieldCheck,
  User,
  Wrench,
  type LucideIcon,
} from "lucide-react"

import { ProviderBadge } from "@/components/identity/provider-badge"
import { cn } from "@/lib/utils"
import {
  toolIconName,
  type ToolIconName,
  type ToolOrigin,
  type ToolStatus,
} from "@/lib/tools"

const ICONS: Record<ToolIconName, LucideIcon> = {
  search: Search,
  pencil: Pencil,
  "message-circle-question": MessageCircleQuestion,
  calendar: Calendar,
  "at-sign": AtSign,
  mail: Mail,
  "hard-drive": HardDrive,
  "file-text": FileText,
  hash: Hash,
  refresh: RefreshCw,
  wrench: Wrench,
}

export function ToolTile({
  tool,
  size = "md",
  className,
}: {
  /** The function reference. */
  tool: string
  size?: "md" | "lg"
  className?: string
}) {
  const Icon = ICONS[toolIconName(tool)]
  return (
    <span
      aria-hidden
      data-slot="tool-tile"
      className={cn(
        "inline-grid shrink-0 place-items-center bg-kind-gray-bg text-kind-gray-fg",
        size === "md" ? "size-8 rounded-[8px]" : "size-11 rounded-[11px]",
        className
      )}
    >
      <Icon className={size === "md" ? "size-[17px]" : "size-[22px]"} />
    </span>
  )
}

const PILL_TONES: Record<ToolStatus["tone"], string> = {
  ok: "bg-ok-soft text-ok",
  warn: "bg-warn-soft text-warning",
  neutral: "bg-hover text-muted-foreground",
}

export function StatusPill({
  status,
  className,
}: {
  status: ToolStatus
  className?: string
}) {
  return (
    <span
      data-slot="status-pill"
      data-tone={status.tone}
      className={cn(
        "inline-flex items-center gap-1.5 rounded-full px-2 py-px text-xs font-medium whitespace-nowrap",
        PILL_TONES[status.tone],
        className
      )}
    >
      {status.tone === "ok" && (
        <span aria-hidden className="size-1.5 rounded-full bg-current" />
      )}
      {status.label}
    </span>
  )
}

export function OriginTag({
  origin,
  className,
}: {
  origin: ToolOrigin
  className?: string
}) {
  let mark: ReactNode
  let words: string
  switch (origin.kind) {
    case "provider":
      mark = <ProviderBadge provider={origin.provider} size="xs" />
      words = `From ${origin.provider.name}`
      break
    case "core":
      mark = <SmallGlyph icon={ShieldCheck} />
      words = "Built into substrate"
      break
    case "yours":
      mark = <SmallGlyph icon={User} />
      words = "Yours"
      break
    default:
      mark = <SmallGlyph icon={Wrench} />
      words = `From ${origin.authority}`
  }
  return (
    <span
      data-slot="origin-tag"
      className={cn(
        "inline-flex items-center gap-1.5 text-[12.5px] whitespace-nowrap text-muted-foreground",
        className
      )}
    >
      {mark}
      {words}
    </span>
  )
}

function SmallGlyph({ icon: Icon }: { icon: LucideIcon }) {
  return (
    <span
      aria-hidden
      className="inline-grid size-4 place-items-center rounded-[4px] bg-kind-gray-bg text-kind-gray-fg"
    >
      <Icon className="size-3" />
    </span>
  )
}
