// The clinical record (docs/plans/healthspan.md, The app: Record). The
// contract the shell, Today and the Record screen read it through, live
// and demo. What has been read is the profile's `recordSeenAt`.

import { useRead, useRecord, useSubstrate } from "@substrate/react"
import { useCallback, useMemo } from "react"
import { demoRecordRows } from "../demo/record"
import { useDemoProfileRecord } from "../demo/profile"
import { KIND, PROFILE_ID, Profile } from "../kinds"
import {
  CLINICAL_KINDS,
  hasRows,
  openStoredFile,
  readRecord,
  toRecord,
  type ClinicalRecord,
  type StoredFile,
} from "../record/read"
import { evaluateRecord, markAllRead } from "../rules/record"
import type { Finding } from "../rules/types"
import { useMode } from "./mode"
import { demoProfile, liveProfile, type ProfileWrites } from "./profile"
import { settled, type Read } from "./read"

/** What the owner has read of the record: everything that arrived at or
 * before `seenAt`. `known` once the profile's read has answered. */
export interface RecordSeen {
  known: boolean
  seenAt?: string
  /** Marks everything that has arrived read. */
  markAllRead: () => Promise<void>
}

export interface RecordData {
  /** Whether the repository holds any clinical record, so the menu shows
   * Record; false until known. */
  useHasRecord: () => boolean
  /** Record items due or new (an appointment within 14 days, a repeat
   * medicine due, a referral past its wait, a document or message since
   * the owner last marked the record read), for Today. */
  useRecordFindings: () => Read<Finding[]>
  /** The whole record, every provider's. */
  useClinicalRecord: () => Read<ClinicalRecord>
  useRecordSeen: () => RecordSeen
  /** Opens a stored file through the blob route; none in the demo, which
   * keeps no files. */
  useOpenFile: () => ((file: StoredFile) => Promise<void>) | undefined
}

function recordData(source: {
  useClinicalRecord: () => Read<ClinicalRecord>
  useSeenAt: () => { known: boolean; seenAt?: string }
  useProfileWrites: () => ProfileWrites
  useOpenFile: () => ((file: StoredFile) => Promise<void>) | undefined
}): RecordData {
  function useRecordSeen(): RecordSeen {
    const record = source.useClinicalRecord().data
    const { known, seenAt } = source.useSeenAt()
    const { change } = source.useProfileWrites()
    const mark = useCallback(async () => {
      const at = record && markAllRead(record, seenAt)
      if (at) await change({ recordSeenAt: at })
    }, [record, seenAt, change])
    return useMemo(
      () => ({ known, seenAt, markAllRead: mark }),
      [known, seenAt, mark]
    )
  }
  return {
    useHasRecord() {
      const record = source.useClinicalRecord().data
      return !!record && hasRows(record)
    },
    useRecordFindings() {
      const record = source.useClinicalRecord()
      const { known, seenAt } = source.useSeenAt()
      const { today } = useMode()
      const data = useMemo(
        () =>
          record.data && known
            ? evaluateRecord(record.data, today, seenAt)
            : undefined,
        [record.data, known, seenAt, today]
      )
      return {
        data,
        error: record.error,
        loading: !data && !record.error,
        refreshing: record.refreshing,
        refresh: record.refresh,
      }
    },
    useClinicalRecord: source.useClinicalRecord,
    useRecordSeen,
    useOpenFile: source.useOpenFile,
  }
}

const RECORD_KEY = "healthspan/record"

export const liveRecord: RecordData = recordData({
  useClinicalRecord: () =>
    useRead(RECORD_KEY, readRecord, {
      kinds: [...CLINICAL_KINDS, KIND.import, KIND.source],
    }),
  useSeenAt() {
    const profile = useRecord(Profile, PROFILE_ID)
    return {
      known: profile.state !== "loading",
      seenAt: profile.value?.recordSeenAt,
    }
  },
  useProfileWrites: liveProfile.useProfileWrites,
  useOpenFile() {
    const { client } = useSubstrate()
    return useCallback(
      (file: StoredFile) => openStoredFile(client, file),
      [client]
    )
  },
})

export const demoRecord: RecordData = recordData({
  useClinicalRecord() {
    const { today, params } = useMode()
    const profile = params.get("profile")
    return useMemo(
      () => settled(toRecord(demoRecordRows(today, { profile }))),
      [today, profile]
    )
  },
  useSeenAt() {
    const profile = useDemoProfileRecord()
    return { known: true, seenAt: profile?.recordSeenAt }
  },
  useProfileWrites: demoProfile.useProfileWrites,
  useOpenFile: () => undefined,
})
