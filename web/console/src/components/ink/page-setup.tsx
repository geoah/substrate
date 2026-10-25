/** A note's page setup: the paper size and orientation every page shares,
 * and the background of the page being drawn on. Every change applies at
 * once and is one undo step, so there is no Apply to forget. */

import { useState } from "react"
import { ChevronDownIcon } from "lucide-react"

import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover"
import { Segmented } from "@/components/ui/segmented"
import {
  INK_BACKGROUNDS,
  INK_PAGE_SIZES,
  orientationOf,
  oriented,
  paperOf,
  sizeLabel,
  type InkBackgroundType,
  type InkOrientation,
  type InkPaperName,
  type InkSize,
} from "@/lib/ink"

type SizeChoice = InkPaperName | "screen" | "custom"

const PAPER_LABELS: Record<InkBackgroundType, string> = {
  blank: "Blank",
  lined: "Lined",
  grid: "Grid",
  dots: "Dots",
}

/** The device's screen in CSS pixels, which an iPad reports in points, the
 * unit the iPad app sizes a screen-sized note in. */
function screenSize(): InkSize | undefined {
  if (typeof window === "undefined" || !window.screen) return undefined
  const { width, height } = window.screen
  return width > 0 && height > 0 ? { width, height } : undefined
}

/** A size in millimetres, at the format's 1240 units to 210 mm. */
function millimetres(size: InkSize): string {
  const mm = (units: number) => Math.round((units * 210) / 1240)
  return `${mm(size.width)} × ${mm(size.height)} mm`
}

export function PageSetup({
  size,
  onSize,
  background,
  onBackground,
  pages,
  inkOutside,
  disabled,
}: {
  size: InkSize
  onSize: (size: InkSize) => void
  background: InkBackgroundType
  onBackground: (background: InkBackgroundType) => void
  /** How many pages a size change applies to. */
  pages: number
  /** Some ink lies past the page as it is now sized. */
  inkOutside: boolean
  disabled?: boolean
}) {
  const orientation = orientationOf(size)
  const paper = paperOf(size)
  const screen = screenSize()
  const onScreen =
    !paper &&
    screen !== undefined &&
    oriented(screen, orientation).width === size.width &&
    oriented(screen, orientation).height === size.height
  // The choice follows the size, so an undo moves it too; Custom stays
  // chosen while its inputs are open, even on a size that is a preset.
  const [customOpen, setCustomOpen] = useState(false)
  const choice: SizeChoice = customOpen
    ? "custom"
    : (paper ?? (onScreen ? "screen" : "custom"))

  const choose = (next: SizeChoice) => {
    setCustomOpen(next === "custom")
    if (next === "custom") return
    const base = next === "screen" ? screen : INK_PAGE_SIZES[next]
    if (base) onSize(oriented(base, orientation))
  }
  const turn = (next: InkOrientation) => onSize(oriented(size, next))
  const custom = (side: "width" | "height", value: string) => {
    const n = Number(value)
    if (Number.isFinite(n) && n >= 100 && n <= 10_000)
      onSize({ ...size, [side]: Math.round(n) })
  }

  const choices: { value: SizeChoice; label: string }[] = [
    { value: "a4", label: "A4" },
    { value: "a5", label: "A5" },
    { value: "letter", label: "US Letter" },
    ...(screen ? [{ value: "screen" as const, label: "This screen" }] : []),
    { value: "custom", label: "Custom" },
  ]

  return (
    <Popover onOpenChange={(open) => !open && setCustomOpen(false)}>
      <PopoverTrigger
        render={
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={disabled}
            aria-label={`Page setup: ${sizeLabel(size)}, ${PAPER_LABELS[background]} paper`}
          />
        }
      >
        {sizeLabel(size)}
        <ChevronDownIcon data-icon="inline-end" />
      </PopoverTrigger>
      <PopoverContent align="start" className="flex w-80 flex-col gap-4">
        <section className="flex flex-col gap-2" aria-label="Page size">
          <h3 className="text-sm font-medium">Page size</h3>
          <Segmented
            label="Page size"
            value={choice}
            options={choices}
            onChange={choose}
          />
          <Segmented
            label="Orientation"
            value={orientation}
            options={[
              { value: "portrait", label: "Portrait" },
              { value: "landscape", label: "Landscape" },
            ]}
            onChange={turn}
          />
          {choice === "custom" ? (
            <div className="flex items-end gap-2">
              <label className="flex flex-col gap-1 text-xs text-muted-foreground">
                Width
                <Input
                  key={size.width}
                  type="number"
                  inputMode="numeric"
                  min={100}
                  max={10000}
                  defaultValue={Math.round(size.width)}
                  onBlur={(e) => custom("width", e.target.value)}
                  className="w-24"
                />
              </label>
              <label className="flex flex-col gap-1 text-xs text-muted-foreground">
                Height
                <Input
                  key={size.height}
                  type="number"
                  inputMode="numeric"
                  min={100}
                  max={10000}
                  defaultValue={Math.round(size.height)}
                  onBlur={(e) => custom("height", e.target.value)}
                  className="w-24"
                />
              </label>
            </div>
          ) : (
            <p className="text-xs text-muted-foreground tabular-nums">
              {millimetres(size)}
            </p>
          )}
          <p className="text-xs text-muted-foreground">
            {pages > 1
              ? `Applies to all ${pages} pages.`
              : "Applies to every page."}{" "}
            {inkOutside &&
              "Some ink is past the edge: it is hidden there and kept in the file."}
          </p>
        </section>
        <section className="flex flex-col gap-2" aria-label="Paper">
          <h3 className="text-sm font-medium">Background for this page</h3>
          <Segmented
            label="Background for this page"
            value={background}
            options={INK_BACKGROUNDS.map((b) => ({
              value: b,
              label: PAPER_LABELS[b],
            }))}
            onChange={onBackground}
          />
        </section>
      </PopoverContent>
    </Popover>
  )
}
