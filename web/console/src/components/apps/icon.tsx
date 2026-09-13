/* eslint-disable react-refresh/only-export-components */
/** A lucide icon by its kebab-case name, as an app declares it: one lazy
 * chunk per distinct name through `DynamicIcon`, and a name the set does not
 * know falls back to the given icon rather than logging a failed import. */

import { DynamicIcon, iconNames, type IconName } from "lucide-react/dynamic"
import type { ComponentType } from "react"

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
