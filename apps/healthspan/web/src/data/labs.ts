// The labs (docs/plans/healthspan.md, The catalogue). The contract Today
// reads lab findings through; the labs workstream implements it, live and
// demo, and adds the reads its own screens need.

import type { Finding } from "../rules/types"
import { pending, settled, type Read } from "./read"

export interface LabsData {
  /** Results outside the lab's range or off target, important tests
   * overdue, for Today. */
  useLabFindings(): Read<Finding[]>
}

export const liveLabs: LabsData = {
  useLabFindings: () => pending(),
}

export const demoLabs: LabsData = {
  useLabFindings: () => settled([]),
}
