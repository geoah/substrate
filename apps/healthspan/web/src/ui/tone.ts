/** The tones the app's marks and words take: green, yellow and red for the
 * target zones and what they judge, the app's accent, a blue for what
 * informs, and neutral. Today's app's names, so the screens' rules port as
 * they are; the kit's own `Tone` (positive, caution, …) names the kit's
 * banners and is not this one. */
export type Tone =
  "neutral" | "accent" | "success" | "warning" | "danger" | "info"

/** A tone's ink, for words and thin marks (lines, markers, rings, edges):
 * darker than its fill in light mode, so it reads at 4.5:1. */
export const INK: Record<Tone, string> = {
  neutral: "var(--sx-label)",
  accent: "var(--sx-accent-text)",
  success: "var(--hs-ok-ink)",
  warning: "var(--hs-warn-ink)",
  danger: "var(--hs-danger-ink)",
  info: "var(--hs-info-ink)",
}

/** A tone's fill, for washes and zone segments. Warning is the zones'
 * yellow; its words take the orange ink, which reads where yellow does
 * not. */
export const FILL: Record<Tone, string> = {
  neutral: "var(--hs-zone-neutral)",
  accent: "var(--sx-accent)",
  success: "var(--sx-green)",
  warning: "var(--sx-yellow)",
  danger: "var(--sx-red)",
  info: "var(--hs-info)",
}

/** A tone's words, as a utility class. */
export const TEXT: Record<Tone, string> = {
  neutral: "text-secondary",
  accent: "text-accent-text",
  success: "text-ok-ink",
  warning: "text-warn-ink",
  danger: "text-danger-ink",
  info: "text-info-ink",
}

/** A tone's tinted chip: a pale wash of its fill under its ink. */
export const CHIP: Record<Tone, string> = {
  neutral: "bg-fill text-secondary",
  accent: "bg-accent/15 text-accent-text",
  success: "bg-ok/15 text-ok-ink",
  warning: "bg-caution/20 text-warn-ink",
  danger: "bg-danger/12 text-danger-ink",
  info: "bg-info/12 text-info-ink",
}

/** A tone's dot or edge, as a utility class. */
export const DOT: Record<Tone, string> = {
  neutral: "bg-secondary",
  accent: "bg-accent",
  success: "bg-ok",
  warning: "bg-caution",
  danger: "bg-danger",
  info: "bg-info",
}
