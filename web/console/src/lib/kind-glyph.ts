/** A kind's glyph: one stable icon and one stable hue, so the same kind looks
 * the same on every surface. The icon comes from the words of the kind's
 * name; the hue from the same words where they have one, else from a hash of
 * the full reference, which never changes while the kind exists. */

import {
  AtSign,
  BookOpen,
  Bot,
  Box,
  Building2,
  Calendar,
  CircleCheck,
  FileText,
  Folder,
  Globe,
  History,
  Mail,
  MessageSquare,
  Pencil,
  Repeat,
  Settings,
  Tag,
  User,
  Users,
  type LucideIcon,
} from "lucide-react"

import { splitKind } from "@/lib/api/http"
import type { KindInfo } from "@/lib/api/types"
import { splitWords } from "@/lib/kind-names"

export const KIND_HUES = [
  "gray",
  "brown",
  "orange",
  "yellow",
  "green",
  "teal",
  "blue",
  "purple",
  "pink",
  "red",
] as const
export type KindHue = (typeof KIND_HUES)[number]

/** Tailwind sees only literal class names, so every hue is spelled out. */
export const HUE_CLASSES: Record<KindHue, { tile: string; ink: string }> = {
  gray: { tile: "bg-kind-gray-bg text-kind-gray-fg", ink: "text-kind-gray-fg" },
  brown: {
    tile: "bg-kind-brown-bg text-kind-brown-fg",
    ink: "text-kind-brown-fg",
  },
  orange: {
    tile: "bg-kind-orange-bg text-kind-orange-fg",
    ink: "text-kind-orange-fg",
  },
  yellow: {
    tile: "bg-kind-yellow-bg text-kind-yellow-fg",
    ink: "text-kind-yellow-fg",
  },
  green: {
    tile: "bg-kind-green-bg text-kind-green-fg",
    ink: "text-kind-green-fg",
  },
  teal: { tile: "bg-kind-teal-bg text-kind-teal-fg", ink: "text-kind-teal-fg" },
  blue: { tile: "bg-kind-blue-bg text-kind-blue-fg", ink: "text-kind-blue-fg" },
  purple: {
    tile: "bg-kind-purple-bg text-kind-purple-fg",
    ink: "text-kind-purple-fg",
  },
  pink: { tile: "bg-kind-pink-bg text-kind-pink-fg", ink: "text-kind-pink-fg" },
  red: { tile: "bg-kind-red-bg text-kind-red-fg", ink: "text-kind-red-fg" },
}

interface GlyphRule {
  icon: LucideIcon
  iconName: string
  hue?: KindHue
}

const rule = (icon: LucideIcon, iconName: string, hue?: KindHue) => ({
  icon,
  iconName,
  hue,
})

/** Keyword → glyph. A name's words are tried last word first, so the word
 * that says what the thing IS wins: `calendareventseries` is a series,
 * `calendarsync` is machinery, `gmailthread` falls through to mail. */
const KEYWORDS: Record<string, GlyphRule> = {
  task: rule(CircleCheck, "circle-check", "blue"),
  issue: rule(CircleCheck, "circle-check", "purple"),
  project: rule(Folder, "folder", "orange"),
  person: rule(User, "user", "teal"),
  user: rule(User, "user", "teal"),
  contact: rule(AtSign, "at-sign", "teal"),
  organization: rule(Building2, "building", "brown"),
  team: rule(Users, "users", "green"),
  group: rule(Users, "users", "green"),
  note: rule(FileText, "file-text", "yellow"),
  document: rule(FileText, "file-text", "yellow"),
  page: rule(FileText, "file-text", "yellow"),
  file: rule(FileText, "file-text", "yellow"),
  transcript: rule(FileText, "file-text", "gray"),
  calendar: rule(Calendar, "calendar", "red"),
  event: rule(Calendar, "calendar", "red"),
  series: rule(Repeat, "repeat", "gray"),
  email: rule(Mail, "mail", "purple"),
  mail: rule(Mail, "mail", "purple"),
  gmail: rule(Mail, "mail", "red"),
  message: rule(MessageSquare, "message-square", "purple"),
  conversation: rule(MessageSquare, "message-square", "purple"),
  chat: rule(MessageSquare, "message-square", "purple"),
  recipe: rule(BookOpen, "book-open", "green"),
  trip: rule(Globe, "globe", "pink"),
  label: rule(Tag, "tag", "gray"),
  account: rule(Settings, "settings", "gray"),
  config: rule(Settings, "settings", "gray"),
  setting: rule(Settings, "settings", "gray"),
  sync: rule(Settings, "settings", "gray"),
  log: rule(History, "history", "gray"),
  agent: rule(Bot, "bot", "purple"),
  bot: rule(Bot, "bot", "purple"),
  scratchpad: rule(Pencil, "pencil", "gray"),
}

const DEFAULT: GlyphRule = rule(Box, "box")

export interface KindGlyphSpec {
  icon: LucideIcon
  /** The lucide name of `icon`, for tests and for anything that must say
   * which icon without rendering it. */
  iconName: string
  hue: KindHue
}

/** FNV-1a over the reference: stable across sessions and builds. */
export function hashHue(reference: string): KindHue {
  let hash = 0x811c9dc5
  for (let i = 0; i < reference.length; i++) {
    hash ^= reference.charCodeAt(i)
    hash = Math.imul(hash, 0x01000193)
  }
  return KIND_HUES[(hash >>> 0) % KIND_HUES.length]
}

export function kindGlyph(kind: KindInfo | string): KindGlyphSpec {
  const reference = typeof kind === "string" ? kind : kind.identity
  const name = typeof kind === "string" ? splitKind(kind).name : kind.name
  const words = splitWords(name) ?? [name]
  let match: GlyphRule = DEFAULT
  for (let i = words.length - 1; i >= 0; i--) {
    const hit = KEYWORDS[words[i]]
    if (hit) {
      match = hit
      break
    }
  }
  return {
    icon: match.icon,
    iconName: match.iconName,
    hue: match.hue ?? hashHue(reference),
  }
}
