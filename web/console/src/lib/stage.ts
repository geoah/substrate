/** A console place's release stage, and what each stage promises: the
 * stage tag's tooltip and the row's accessible description say the same. */

export type Stage = "alpha" | "beta"

export function stageNote(stage: Stage): string {
  return stage === "alpha"
    ? "Alpha: early, and may change or break"
    : "Beta: usable, and still changing"
}
