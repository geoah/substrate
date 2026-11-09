// The age score and the signals (docs/plans/healthspan.md, The age score
// from the shared kinds). The contract Today reads its findings through;
// the healthspan workstream implements it, live and demo, and adds the
// reads its own screens need.

import type { Finding } from "../rules/types"
import { pending, settled, type Read } from "./read"

export interface HealthspanData {
  /** A contributor that slipped, a signal out of its band, for Today. */
  useHealthspanFindings(): Read<Finding[]>
}

export const liveHealthspan: HealthspanData = {
  useHealthspanFindings: () => pending(),
}

export const demoHealthspan: HealthspanData = {
  useHealthspanFindings: () => settled([]),
}
