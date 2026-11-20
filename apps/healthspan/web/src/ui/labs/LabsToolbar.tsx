import {
  activeSecondary,
  SCOPES,
  STATUSES,
  type FilterCounts,
  type LabsFilters,
} from "../../labs/labsFilters"
import type { DisplayControl } from "../../lib/display"
import { RANGES } from "../../lib/timeDomain"
import { DetailsSwitch, DisplaySettings } from "../DisplaySettings"
import { MobileFilters, MobileGroup } from "../Filters"
import { SearchInput } from "../SearchInput"
import { useShell } from "../shell"
import { SegmentedControl, ToggleGroup } from "../ToggleGroup"
import { Toolbar } from "../Toolbar"
import { FilterSelect } from "./FilterSelect"

/** The Labs toolbar: the search, the scope, then status, range, panel and
 * source; Details and the display settings at its end. On a phone the
 * search, the scope and Details stay out and the rest sit in the Filters
 * sheet with the display settings. */
export function LabsToolbar(props: {
  filters: LabsFilters
  set: (next: Partial<LabsFilters>) => void
  counts: FilterCounts
  panels: string[]
  sources: { value: string; label: string }[]
  control: DisplayControl
}) {
  const { wide } = useShell()
  const { filters, set, counts } = props
  const search = (
    <SearchInput
      value={filters.q}
      onChange={(q) => set({ q })}
      placeholder="Search metrics"
      ariaLabel="Search metrics"
    />
  )
  const scope = (
    <SegmentedControl
      label="Scope"
      options={SCOPES.map((s) => ({ ...s, count: counts.scope[s.value] }))}
      value={filters.scope}
      onChange={(s) => set({ scope: s })}
    />
  )
  const status = (
    <ToggleGroup
      label="Status"
      options={STATUSES.map((s) => ({ ...s, count: counts.status[s.value] }))}
      value={filters.status}
      onChange={(s) => set({ status: s })}
    />
  )
  const range = (
    <SegmentedControl
      label="Range"
      options={RANGES.map((r) => ({ value: r.id, label: r.id }))}
      value={filters.range}
      onChange={(r) => set({ range: r })}
    />
  )
  const panel = (block?: boolean) => (
    <FilterSelect
      label="Panel"
      block={block}
      value={filters.panel ?? ""}
      onChange={(v) => set({ panel: v || undefined })}
      options={[
        { value: "", label: "All panels" },
        ...props.panels.map((p) => ({ value: p, label: p })),
      ]}
    />
  )
  const source = (block?: boolean) => (
    <FilterSelect
      label="Source"
      block={block}
      value={filters.source ?? ""}
      onChange={(v) => set({ source: v || undefined })}
      options={[{ value: "", label: "All sources" }, ...props.sources]}
    />
  )

  if (!wide)
    return (
      <Toolbar
        search={search}
        end={<DetailsSwitch page="labs" control={props.control} />}
      >
        {scope}
        <MobileFilters
          page="labs"
          control={props.control}
          activeCount={activeSecondary(filters)}
        >
          <MobileGroup label="Status">{status}</MobileGroup>
          <MobileGroup label="Range">{range}</MobileGroup>
          {panel(true)}
          {source(true)}
        </MobileFilters>
      </Toolbar>
    )
  return (
    <Toolbar
      search={search}
      end={
        <>
          <DetailsSwitch page="labs" control={props.control} />
          <DisplaySettings page="labs" control={props.control} />
        </>
      }
    >
      {scope}
      {status}
      {range}
      {panel()}
      {source()}
    </Toolbar>
  )
}
