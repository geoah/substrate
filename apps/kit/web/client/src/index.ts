export { SubstrateClient } from "./client"
export type { Query, RequestOptions } from "./client"
export {
  SubstrateError,
  describe,
  failure,
  isNetworkError,
  retryAfterSeconds,
} from "./errors"
export type { ProblemDetail } from "./errors"
export {
  booleanProp,
  numberProp,
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
export { defineKind, parseKind } from "./kinds"
export type { Kind, KindCodec, KindName } from "./kinds"
export { follow, parseWatchFrame } from "./watch"
export type {
  FollowOptions,
  WatchCursor,
  WatchEvent,
  WatchFrame,
  WatchRow,
} from "./watch"
export { uuidv7 } from "./ids"
export { boundTrait, kindStatus, lifecycleTrait, moveTo, movesFrom, renames, standing } from "./status"
export type { KindStatus, Standing } from "./status"
export { comparePositions, positionAfter, positionBetween } from "./position"
export {
  NotASubstrate,
  ServerTooOld,
  inspectServer,
  isLoopback,
  locate,
  originOf,
  parseSubstrateTXT,
  probe,
  recordServer,
} from "./discovery"
export type {
  AuthorizationServerMetadata,
  LocatedServer,
  ServerDocument,
} from "./discovery"
export {
  compareServerVersions,
  displayServerVersion,
  formatServerVersion,
  isRelease,
  judgeServer,
  parseServerVersion,
} from "./version"
export type { ServerRequirement, ServerVerdict, ServerVersion } from "./version"
export {
  blockServerUpdate,
  setupActionsNeedingApproval,
} from "./authorizations"
export type {
  AuthorizationBlock,
  AuthorizationCheck,
  AuthorizationCheckResult,
  AuthorizationDetail,
  AuthorizationSetup,
} from "./authorizations"
