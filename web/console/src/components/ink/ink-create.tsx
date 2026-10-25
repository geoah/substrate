/** A new note, drawn first: New opens drawing mode on a fresh page, the name
 * already filled in with when it was started (editable, and never focused,
 * so a tablet's keyboard stays down), and Save creates the record with its
 * file in one step. Nothing exists until Save; Cancel after drawing asks
 * before the drawing goes. */

import { useState } from "react"
import { useQueryClient } from "@tanstack/react-query"
import { useNavigate } from "@tanstack/react-router"

import { toast } from "@/components/ui/toast"
import { putBlob } from "@/lib/api/blobs"
import { createRecord } from "@/lib/api/records"
import type { KindInfo } from "@/lib/api/types"
import {
  INK_MEDIA_TYPE,
  backgroundType,
  inkText,
  newInkDocument,
  type InkDocument,
} from "@/lib/ink"
import { defaultNoteName, inkFileName } from "@/lib/ink-record"
import { readInkSetup, writeInkSetup } from "@/lib/ink-setup"
import { displayName, lowerFirst } from "@/lib/kind-names"
import type { PropSpec } from "@/lib/record-schema"

import { InkEditor } from "./ink-editor"

export function InkCreate({
  authority,
  pkg,
  name,
  kind,
  ink,
  heading,
}: {
  authority: string
  pkg: string
  name: string
  kind: KindInfo
  /** The property the note's file goes in. */
  ink: PropSpec
  /** The property the name is typed into, where the kind has one. */
  heading?: PropSpec
}) {
  const navigate = useNavigate()
  const client = useQueryClient()
  const [started] = useState(() => defaultNoteName(new Date()))
  const [title, setTitle] = useState(started)
  const [initial] = useState(() => {
    const setup = readInkSetup()
    return newInkDocument({ size: setup.size, background: setup.background })
  })
  const [saving, setSaving] = useState(false)
  const [failure, setFailure] = useState<string | undefined>()
  const noun = lowerFirst(displayName(kind))

  const leave = () =>
    void navigate({
      to: "/data/$authority/$pkg/$name",
      params: { authority, pkg, name },
    })

  const save = async (document: InkDocument) => {
    setSaving(true)
    setFailure(undefined)
    const named = title.trim() || started
    try {
      const info = await putBlob(inkText(document), {
        name: inkFileName(named, ink.name),
        mediaType: INK_MEDIA_TYPE,
      })
      const properties: Record<string, unknown> = { [ink.name]: info.digest }
      if (heading) properties[heading.name] = named
      const saved = await createRecord(authority, pkg, name, { properties })
      writeInkSetup({
        size: { width: document.page.width, height: document.page.height },
        background: backgroundType(document.pages[0]?.background),
      })
      toast.add({ type: "success", title: `${displayName(kind)} created` })
      void client.invalidateQueries()
      void navigate({
        to: "/data/$authority/$pkg/$name/$id",
        params: { authority, pkg, name, id: saved.id },
      })
    } catch (e) {
      const reason = (e instanceof Error ? e.message : String(e)).replace(
        /[.\s]*$/,
        ""
      )
      setFailure(
        `This ${noun} was not saved: ${reason}. Your drawing is still open.`
      )
      setSaving(false)
    }
  }

  return (
    <InkEditor
      initial={initial}
      allowPristineSave
      saving={saving}
      changed={title !== started}
      discard={{
        title: `Discard this ${noun}?`,
        consequence: "Nothing has been saved, so the drawing goes.",
      }}
      onSave={(document) => void save(document)}
      onCancel={leave}
      heading={
        heading ? (
          <input
            aria-label={heading.label}
            value={title}
            placeholder={started}
            onChange={(e) => setTitle(e.target.value)}
            disabled={saving}
            className="w-full min-w-0 truncate rounded-md bg-transparent px-1 py-1 text-base font-medium outline-none focus-visible:ring-3 focus-visible:ring-ring/50"
          />
        ) : (
          <span className="block truncate text-sm font-medium">New {noun}</span>
        )
      }
      note={
        failure ? (
          <span className="text-destructive" role="alert">
            {failure}
          </span>
        ) : (
          `A new ${noun}: Save creates it.`
        )
      }
    />
  )
}
