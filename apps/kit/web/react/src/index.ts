// An app's hello world imports this package alone, so the lower layers'
// kind declaration, property readers and manifest are re-exported here.
export {
  booleanProp,
  defineKind,
  numberProp,
  refProp,
  stringProp,
} from "@substrate/client"
export type { Kind, SubstrateRecord } from "@substrate/client"
export {
  NotSignedIn,
  NotSignedOut,
  packageDetail,
  webManifest,
} from "@substrate/auth"
export type { AppManifest } from "@substrate/auth"

export { SubstrateApp, Spinner } from "./SubstrateApp"
export type { SubstrateAppProps } from "./SubstrateApp"
export { SubstrateProvider } from "./provider"
export type { SubstrateProviderProps } from "./provider"
export { useConnection, useSubstrate } from "./context"
export type { Connection, SubstrateContextValue } from "./context"
export {
  useAppStatus,
  useInvalidate,
  useLinkedStatus,
  useListed,
  useOutbox,
  useParkedWrites,
  useRead,
  useRecord,
  useRecords,
  useStatusSentence,
  useWriteState,
} from "./hooks"
export type { RecordRead } from "./hooks"
export { sampleKinds } from "./reads"
export type { Read, ReadCache, ReadOptions, ReadState } from "./reads"
export { useKindWrites, useWrites } from "./writes"
export type { KindWrites, WriteOptions, WriteResult, Writes } from "./writes"
export { ConnectScreen } from "./ConnectScreen"
export { AccessBanner, RefusedBanner } from "./AccessBanner"
export {
  AboutSection,
  ApprovedAccessSection,
  RefusedWritesSection,
  RepositorySection,
  StatusSection,
  StorageSection,
  SubstrateSettings,
  byteWords,
} from "./settings"
export {
  deriveLinkStatus,
  formatDay,
  formatMoment,
  statusSentence,
  statusTone,
} from "./status"
export type { LinkStatus as LinkState, StatusWords } from "./status"
export { ToastHost, useToast } from "./toast"
export { useMediaQuery } from "./media"
export {
  ActionRow,
  Alert,
  BarButton,
  Confirm,
  cx,
  Field,
  LargeTitle,
  LinkStatus,
  NavBar,
  NoteRow,
  ProblemBox,
  Row,
  Section,
  SelectRow,
  Sheet,
  SidebarItem,
  TabBar,
  TabItem,
  Toggle,
  ToggleRow,
  VerbatimRow,
} from "./ui"
export type { AlertButton, Tone } from "./ui"
