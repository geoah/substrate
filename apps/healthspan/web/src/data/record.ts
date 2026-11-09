// The clinical record (docs/plans/healthspan.md, The app: Record). The
// contract the shell and Today read it through; the record workstream
// implements it, live and demo.

import type { Finding } from "../rules/types"
import { pending, settled, type Read } from "./read"

export interface RecordData {
  /** Whether the repository holds any clinical record, so the menu shows
   * Record; false until known. */
  useHasRecord(): boolean
  /** Record items due or new (an appointment within 14 days, a repeat
   * medicine due, a referral past its wait, a document or message since
   * the owner last marked the record read), for Today. */
  useRecordFindings(): Read<Finding[]>
}

export const liveRecord: RecordData = {
  useHasRecord: () => false,
  useRecordFindings: () => pending(),
}

export const demoRecord: RecordData = {
  useHasRecord: () => true,
  useRecordFindings: () => settled([]),
}
