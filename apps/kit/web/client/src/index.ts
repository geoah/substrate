export { SubstrateClient } from "./client"
export type { Query, RequestOptions } from "./client"
export { SubstrateError, describe, failure } from "./errors"
export type { ProblemDetail } from "./errors"
export {
  parseReference,
  recordKey,
  referencePath,
  refProp,
  stringProp,
} from "./records"
export type {
  ListQuery,
  RecordPage,
  RecordReference,
  SubstrateRecord,
} from "./records"
export { follow } from "./watch"
export type { WatchCursor, WatchEvent, WatchRow } from "./watch"
export { uuidv7 } from "./ids"
export { comparePositions, positionAfter, positionBetween } from "./position"
export {
  NotASubstrate,
  isLoopback,
  locate,
  originOf,
  probe,
  recordServer,
} from "./discovery"
export type {
  AuthorizationServerMetadata,
  LocatedServer,
  ServerDocument,
} from "./discovery"
