import { cx } from "@substrate/react"
import { TEXT, type Tone } from "../tone"

export interface StatTile {
  label: string
  value: string
  unit?: string
  /** The line under the value. */
  sub?: string
  /** The value's colour; status is never colour alone, `sub` says it. */
  tone?: Tone
  /** While the tile shows a hovered point rather than the newest. */
  isSelected?: boolean
}

/** A history's facts in tiles, three across from 640 px; on a phone the
 * first across the width and the others two by two. Each has its label
 * over its value and its line under it. */
export function StatTiles({ tiles }: { tiles: StatTile[] }) {
  return (
    <dl className="mb-6 grid grid-cols-2 gap-3 sm:grid-cols-3">
      {tiles.map((t) => (
        <div
          key={t.label}
          data-selected={t.isSelected ? "true" : undefined}
          className="flex min-w-0 flex-col rounded-[10px] bg-card px-4 py-3 first:col-span-2 sm:first:col-span-1"
        >
          <dt className="text-[13px] text-secondary">{t.label}</dt>
          <dd className="mt-0.5 min-w-0">
            <span
              className={cx(
                "text-[20px] leading-[30px] font-semibold tabular-nums sm:text-[24px]",
                t.tone && TEXT[t.tone]
              )}
            >
              {t.value}
            </span>
            {t.unit && (
              <span className="text-[13px] text-secondary">
                {/^[%°]/.test(t.unit) ? t.unit : ` ${t.unit}`}
              </span>
            )}
          </dd>
          <dd
            className="min-h-[19px] text-[13px] leading-[19px] text-secondary"
            aria-live={t.isSelected !== undefined ? "polite" : undefined}
          >
            {t.sub ?? ""}
          </dd>
        </div>
      ))}
    </dl>
  )
}
