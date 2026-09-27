/** The kind's tile: its stable icon on its stable hue. Every surface that
 * marks a kind or a record of one draws this, never an icon of its own. */

import type { KindInfo } from "@/lib/api/types"
import { HUE_CLASSES, kindGlyph } from "@/lib/kind-glyph"
import { cn } from "@/lib/utils"

export type GlyphSize = "xs" | "sm" | "md" | "lg"

const SIZES: Record<GlyphSize, { tile: string; icon: string }> = {
  xs: { tile: "size-4 rounded-[4px]", icon: "size-[11px]" },
  sm: { tile: "size-5 rounded-[5px]", icon: "size-[13px]" },
  md: { tile: "size-7 rounded-[7px]", icon: "size-4" },
  lg: { tile: "size-10 rounded-[10px]", icon: "size-[22px]" },
}

export function KindGlyph({
  kind,
  size = "sm",
  className,
}: {
  /** A registry entry or a full kind reference. */
  kind: KindInfo | string
  size?: GlyphSize
  className?: string
}) {
  const { icon: Icon, hue } = kindGlyph(kind)
  const s = SIZES[size]
  return (
    <span
      aria-hidden
      data-slot="kind-glyph"
      className={cn(
        "inline-grid shrink-0 place-items-center",
        s.tile,
        HUE_CLASSES[hue].tile,
        className
      )}
    >
      <Icon className={s.icon} strokeWidth={2} />
    </span>
  )
}
