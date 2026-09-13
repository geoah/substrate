/** The icons a row action or a chevron draws, by the kebab-case lucide name
 * an app writes (`{icon: "mail"}`). A fixed set imported by name so the kit
 * chunk carries these and not the whole library; a name outside it draws
 * its first letter rather than failing the row. */

import {
  ArchiveIcon,
  ArrowUpRightIcon,
  BellIcon,
  CalendarIcon,
  CheckIcon,
  ChevronRightIcon,
  CircleAlertIcon,
  ClockIcon,
  CopyIcon,
  EllipsisIcon,
  ExternalLinkIcon,
  GlobeIcon,
  LinkIcon,
  MailIcon,
  MapPinIcon,
  MessageSquareIcon,
  PencilIcon,
  PhoneIcon,
  PlusIcon,
  RefreshCwIcon,
  SearchIcon,
  SendIcon,
  ShareIcon,
  StarIcon,
  TagIcon,
  Trash2Icon,
  UserIcon,
  XIcon,
  type LucideProps,
} from "lucide-react"
import type { ComponentType } from "react"

const ICONS: Record<string, ComponentType<LucideProps>> = {
  archive: ArchiveIcon,
  "arrow-up-right": ArrowUpRightIcon,
  bell: BellIcon,
  calendar: CalendarIcon,
  check: CheckIcon,
  "chevron-right": ChevronRightIcon,
  "circle-alert": CircleAlertIcon,
  clock: ClockIcon,
  copy: CopyIcon,
  ellipsis: EllipsisIcon,
  "external-link": ExternalLinkIcon,
  globe: GlobeIcon,
  link: LinkIcon,
  mail: MailIcon,
  "map-pin": MapPinIcon,
  "message-square": MessageSquareIcon,
  pencil: PencilIcon,
  phone: PhoneIcon,
  plus: PlusIcon,
  "refresh-cw": RefreshCwIcon,
  search: SearchIcon,
  send: SendIcon,
  share: ShareIcon,
  star: StarIcon,
  tag: TagIcon,
  trash: Trash2Icon,
  "trash-2": Trash2Icon,
  user: UserIcon,
  x: XIcon,
}

export function KitIcon({ name, size = 20 }: { name: string; size?: number }) {
  const Icon = ICONS[name]
  if (!Icon) {
    return (
      <span className="kit-icon-fallback" aria-hidden>
        {name.charAt(0).toUpperCase()}
      </span>
    )
  }
  return <Icon size={size} strokeWidth={1.75} aria-hidden />
}
