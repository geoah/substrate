import { initialsOf } from "./buckets"

/** Initials in a circle; `size` in pixels. */
export function Avatar({ name, size = 40 }: { name: string; size?: number }) {
  const initials = initialsOf(name) || "?"
  return (
    <span
      className="kit-avatar"
      role="img"
      aria-label={name}
      style={{ width: size, height: size, fontSize: Math.round(size * 0.4) }}
    >
      {initials}
    </span>
  )
}
