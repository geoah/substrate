import { cx } from "@substrate/react"

/** One placeholder block while content loads, sized to what it stands in
 * for so nothing moves when the content lands. Hidden from assistive
 * technology: the screen says it is loading in words. */
export function Skeleton({
  width = "100%",
  height = 12,
  radius = 4,
  className,
}: {
  width?: number | string
  height?: number | string
  radius?: number
  className?: string
}) {
  return (
    <span
      aria-hidden
      className={cx("block animate-pulse bg-fill", className)}
      style={{ width, height, borderRadius: radius }}
    />
  )
}

/** A paragraph's placeholder: `lines` rows, the last one shorter. */
export function SkeletonText({
  lines = 3,
  lastLineWidth = "60%",
}: {
  lines?: number
  lastLineWidth?: number | string
}) {
  const n = Math.max(1, lines)
  return (
    <span className="flex flex-col gap-2" aria-hidden>
      {Array.from({ length: n }, (_, i) => (
        <Skeleton key={i} width={i === n - 1 ? lastLineWidth : "100%"} />
      ))}
    </span>
  )
}

/** A list row's placeholder: a square beside a few lines. */
export function SkeletonRow({
  lines = 2,
  hasAvatar = true,
}: {
  lines?: number
  hasAvatar?: boolean
}) {
  return (
    <span className="flex items-center gap-3 px-4 py-2.5" aria-hidden>
      {hasAvatar && <Skeleton width={32} height={32} radius={8} />}
      <span className="flex flex-1 flex-col gap-2">
        {Array.from({ length: Math.max(1, lines) }, (_, i) => (
          <Skeleton key={i} width={i === 0 ? "45%" : "80%"} />
        ))}
      </span>
    </span>
  )
}

/** A metric card's placeholder: the title, the value, two lines and the
 * chart's height, so a grid of them holds the page while it loads. */
export function SkeletonCard({ chartHeight = 160 }: { chartHeight?: number }) {
  return (
    <div
      className="flex flex-col gap-2 rounded-[10px] bg-card px-4 pt-3 pb-4"
      aria-hidden
    >
      <Skeleton width="50%" height={16} />
      <Skeleton width="35%" height={24} />
      <Skeleton width="80%" />
      <Skeleton height={chartHeight} radius={6} className="mt-1" />
    </div>
  )
}
