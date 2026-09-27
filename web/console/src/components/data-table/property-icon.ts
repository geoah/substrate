/** The icon a property wears wherever it is named: a grid column's header and
 * the filter bar's list of what can be filtered. One map, so a property reads
 * the same in both. */

import {
  ArrowUpRightIcon,
  AtSignIcon,
  CalendarIcon,
  CircleDotIcon,
  GlobeIcon,
  HashIcon,
  LinkIcon,
  ListIcon,
  PhoneIcon,
  RepeatIcon,
  SquareCheckIcon,
  TypeIcon,
  type LucideIcon,
} from "lucide-react"

import type { DeclaredProperty } from "@/lib/definition"

const ICONS: Record<string, LucideIcon> = {
  state: CircleDotIcon,
  enum: ListIcon,
  reference: ArrowUpRightIcon,
  datetime: CalendarIcon,
  date: CalendarIcon,
  email: AtSignIcon,
  url: LinkIcon,
  phone: PhoneIcon,
  int: HashIcon,
  float: HashIcon,
  decimal: HashIcon,
  bool: SquareCheckIcon,
  timezone: GlobeIcon,
  recurrence: RepeatIcon,
}

export function propertyIcon(prop: Pick<DeclaredProperty, "kind">): LucideIcon {
  return ICONS[prop.kind] ?? TypeIcon
}
