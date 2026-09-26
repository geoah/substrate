/** A tool's icon tile. Where a tool comes from is an `OriginMark`. */

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
  Wrench,
  type LucideIcon,
} from "lucide-react"

import { cn } from "@/lib/utils"
import { toolIconName, type ToolIconName } from "@/lib/tools"

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
