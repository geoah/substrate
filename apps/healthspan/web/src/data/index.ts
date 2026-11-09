import {
  demoHealthspan,
  liveHealthspan,
  type HealthspanData,
} from "./healthspan"
import { demoLabs, liveLabs, type LabsData } from "./labs"
import { useMode } from "./mode"
import { demoProfile, liveProfile, type ProfileData } from "./profile"
import { demoRecord, liveRecord, type RecordData } from "./record"
import { demoSources, liveSources, type SourcesData } from "./sources"

/** Every read and write the screens make, one domain each, live on the
 * owner's substrate or on the demo member. A screen takes a domain's hooks
 * from here and calls them unconditionally: the mode is fixed for the
 * page's life, so the same hooks run on every render. */
export interface Backend {
  labs: LabsData
  healthspan: HealthspanData
  record: RecordData
  profile: ProfileData
  sources: SourcesData
}

const LIVE: Backend = {
  labs: liveLabs,
  healthspan: liveHealthspan,
  record: liveRecord,
  profile: liveProfile,
  sources: liveSources,
}

const DEMO: Backend = {
  labs: demoLabs,
  healthspan: demoHealthspan,
  record: demoRecord,
  profile: demoProfile,
  sources: demoSources,
}

export function useBackend(): Backend {
  return useMode().demo ? DEMO : LIVE
}
