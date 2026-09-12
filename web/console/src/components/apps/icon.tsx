/* eslint-disable react-refresh/only-export-components */
/** A lucide icon by its kebab-case name, as an app, a screen or an action
 * declares it: one lazy chunk per distinct name through `DynamicIcon`, and a
 * name the set does not know falls back to the given icon rather than
 * logging a failed import. */

import {
  CalendarRangeIcon,
  ClipboardListIcon,
  CodeIcon,
  ContactIcon,
  FileTextIcon,
  KanbanIcon,
  ListIcon,
  type LucideProps,
} from "lucide-react"
import { DynamicIcon, iconNames, type IconName } from "lucide-react/dynamic"
import type { ComponentType } from "react"

import type { Layout } from "@/lib/apps/spec"

/** One icon per layout, for a view that declares none of its own. */
export const LAYOUT_ICONS: Record<Layout, ComponentType<LucideProps>> = {
  list: ListIcon,
  board: KanbanIcon,
  timeline: CalendarRangeIcon,
  contacts: ContactIcon,
  detail: FileTextIcon,
  form: ClipboardListIcon,
  custom: CodeIcon,
}

const known = new Set<string>(iconNames)

export function isIconName(name: unknown): name is IconName {
  return typeof name === "string" && known.has(name)
}

export function AppIcon({
  name,
  fallback: Fallback,
  className,
}: {
  name?: string
  fallback: ComponentType<{ className?: string }>
  className?: string
}) {
  if (!isIconName(name)) return <Fallback className={className} />
  return (
    <DynamicIcon
      name={name}
      className={className}
      fallback={() => <Fallback className={className} />}
    />
  )
}
