/** `substrate/ui`: the kit an app imports beside `substrate/app`. Plain React
 * over the console's tokens, built as its own entry so the guest's import map
 * can name it. Every name is exported from day one; the v1 ones render a
 * note until they land, so an import never fails to resolve. */

import { ensureKitStyles } from "./styles"

export { Avatar } from "./avatar"
export { Badge, StateBadge, type BadgeTone } from "./badge"
export { Check, type CheckProps } from "./check"
export { Empty, type EmptyProps } from "./empty"
export { Group, type GroupProps } from "./group"
export { List, type ListPage, type ListProps } from "./list"
export { QuickAdd, type QuickAddProps } from "./quick-add"
export { Row, type RowAction, type RowProps } from "./row"
export { Screen, type ScreenProps } from "./screen"
export { Spinner } from "./spinner"
export { Timeline, type TimelineProps } from "./timeline"
export {
  Board,
  Button,
  Form,
  RecordField,
  Sheet,
  type BoardProps,
  type ButtonProps,
  type FormProps,
  type RecordFieldProps,
  type SheetProps,
} from "./later"
export {
  BUCKETS,
  bucketOf,
  dayKey,
  dayLabel,
  groupRecords,
  relativeDay,
  titleOf,
  type Bucket,
  type RecordGroup,
} from "./buckets"

ensureKitStyles()
