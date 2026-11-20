import { ActionRow, Section, Toggle } from "@substrate/react"
import { SlidersHorizontal } from "lucide-react"
import { useEffect, useState } from "react"
import {
  isChanged,
  mainWidth,
  resetPatch,
  wideAddsColumns,
  type DisplayControl,
  type DisplayPage,
  type HealthspanDisplay,
  type LabsDisplay,
} from "../lib/display"
import { IconButton } from "./IconButton"
import { Popover } from "./Popover"

export type { DisplayPage }

export interface DisplayRow<K extends string> {
  key: K
  label: string
  /** One line under the label. */
  description: string
}

export const LABS_DISPLAY_ROWS: DisplayRow<keyof LabsDisplay>[] = [
  {
    key: "details",
    label: "Details",
    description: "Source counts, ranges and direction under every chart",
  },
  {
    key: "trendline",
    label: "Trendline",
    description: "Best-fit line over the shown dates",
  },
  {
    key: "targetZones",
    label: "Target zones",
    description: "Good, borderline and poor zones from the catalogue",
  },
  {
    key: "labRange",
    label: "Lab range",
    description: "The latest result's lab range",
  },
  {
    key: "sourceShapes",
    label: "Source shapes",
    description: "A marker shape per source; off draws circles",
  },
  { key: "key", label: "Key", description: "The chart key under the filters" },
  {
    key: "groupByPanel",
    label: "Group by panel",
    description: "Sections per panel; off is one grid sorted by status",
  },
]

export const HEALTHSPAN_DISPLAY_ROWS: DisplayRow<keyof HealthspanDisplay>[] = [
  {
    key: "details",
    label: "Details",
    description: "The 182-day chart and the verdict under every card",
  },
  { key: "key", label: "Key", description: "The key under the filters" },
  {
    key: "trendView",
    label: "Trend view",
    description: "Pace of aging and age score by week",
  },
]

function rowsOf(page: DisplayPage): DisplayRow<string>[] {
  return page === "labs" ? LABS_DISPLAY_ROWS : HEALTHSPAN_DISPLAY_ROWS
}

/** The Wide view row, one value for both screens. Where the main area has
 * no room for more columns it says so, and stays usable. */
const WIDE_ROW = {
  label: "Wide view",
  description:
    "The screen uses the whole window, with more and narrower columns",
  noRoom: "No extra room at this width",
}

/** The window's width, kept current. */
function useWindowWidth(): number {
  const [w, setW] = useState(() =>
    typeof window === "undefined" ? 0 : window.innerWidth
  )
  useEffect(() => {
    const on = () => setW(window.innerWidth)
    window.addEventListener("resize", on)
    return () => window.removeEventListener("resize", on)
  }, [])
  return w
}

/** One switch with its label over its description: the description wraps
 * under the label at any length. */
function SwitchRow(props: {
  label: string
  description: string
  checked: boolean
  onChange: (v: boolean) => void
}) {
  return (
    <div className="flex min-h-11 items-center gap-3 px-4 py-2">
      <div className="min-w-0 flex-1">
        <p className="text-[17px]">{props.label}</p>
        <p className="text-[13px] text-secondary">{props.description}</p>
      </div>
      <Toggle
        label={props.label}
        checked={props.checked}
        onChange={props.onChange}
      />
    </div>
  )
}

/** A screen's switches, the Wide view, then "Reset to defaults" (the
 * screen's switches and the wide view). */
function DisplayRows({
  page,
  control,
}: {
  page: DisplayPage
  control: DisplayControl
}) {
  const values = control.display[page] as unknown as Record<string, boolean>
  const width = mainWidth(useWindowWidth())
  return (
    // The kit's sections keep a space under them; the last one here none.
    <div className="[&>section:last-child]:mb-0">
      <Section>
        {rowsOf(page).map((r) => (
          <SwitchRow
            key={r.key}
            label={r.label}
            description={r.description}
            checked={values[r.key]}
            onChange={(v) => control.setDisplay({ [page]: { [r.key]: v } })}
          />
        ))}
        <SwitchRow
          label={WIDE_ROW.label}
          description={
            wideAddsColumns(width) ? WIDE_ROW.description : WIDE_ROW.noRoom
          }
          checked={control.display.wide}
          onChange={(wide) => control.setDisplay({ wide })}
        />
      </Section>
      <Section>
        <ActionRow
          onClick={() => control.setDisplay(resetPatch(page))}
          disabled={!isChanged(control.display, page)}
        >
          Reset to defaults
        </ActionRow>
      </Section>
    </div>
  )
}

export interface DisplaySettingsProps {
  page: DisplayPage
  control: DisplayControl
  /** `popover` (default): the toolbar's sliders button and its panel, a
   * Sheet on a phone. `inline`: the rows alone, for the Filters sheet. */
  variant?: "popover" | "inline"
}

/** A screen's display settings: a sliders button, marked while any of the
 * screen's settings differs from its default, opening the switches. */
export function DisplaySettings({
  page,
  control,
  variant = "popover",
}: DisplaySettingsProps) {
  if (variant === "inline") return <DisplayRows page={page} control={control} />
  return (
    <Popover
      title="Display"
      align="end"
      panelClassName="bg-grouped w-[min(24rem,calc(100vw-2rem))] px-3"
      trigger={({ isOpen, triggerProps }) => (
        <IconButton
          label="Display settings"
          active={isOpen || isChanged(control.display, page)}
          {...triggerProps}
        >
          <SlidersHorizontal className="size-[18px]" aria-hidden />
        </IconButton>
      )}
    >
      <DisplayRows page={page} control={control} />
    </Popover>
  )
}

/** The one-tap Details switch beside the toolbar's buttons: the same value
 * as the first display row. */
export function DetailsSwitch({
  page,
  control,
}: {
  page: DisplayPage
  control: DisplayControl
}) {
  return (
    <label className="inline-flex shrink-0 items-center gap-2 text-[15px]">
      <span>Details</span>
      <Toggle
        label="Details"
        checked={control.display[page].details}
        onChange={(details) => control.setDisplay({ [page]: { details } })}
      />
    </label>
  )
}
