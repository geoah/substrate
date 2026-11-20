// The range bar's ramp: ten segments stepping from the costly end to the
// rewarding end, four steps each way, neutral where a value neither adds
// nor removes years. The bars draw it in the kit's tones
// (healthspan/segmentTones.ts); its test holds the tones to this ramp.

export const BAD = ["#69512a", "#926928", "#be8126", "#ffa722"]
export const GOOD = ["#0d5e47", "#118e67", "#0bb77d", "#00f29e"]
export const NEUTRAL = "#969696"

/** Segment colours for one bar from the years at each segment's midpoint.
 * Grey where a value adds or removes almost nothing; otherwise four steps of
 * orange or teal relative to this bar's own extremes, so a bar ends in full
 * orange or full teal wherever its curve has one. */
export function segmentColours(years: number[]): string[] {
  const worst = Math.max(0, ...years)
  const best = Math.min(0, ...years)
  return years.map((y) => {
    if (Math.abs(y) < 0.025) return NEUTRAL
    const ramp = y > 0 ? BAD : GOOD
    const ratio = y > 0 ? y / worst : y / best
    return ramp[Math.min(3, Math.floor(ratio * 4 - 1e-9))]
  })
}
