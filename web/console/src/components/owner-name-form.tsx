/** The owner's name, as one field: registration asks it once the repository
 * exists, and Settings, Your name asks it of a repository registered before
 * that step. Saving creates the owner's contact and points the core setting
 * at it (decision record 0231); where the setting already names a live
 * contact, saving renames that contact instead of creating a second one.
 *
 * The contact's id is held once it is created, so a retry after a failed
 * setting write points the setting at that contact rather than creating
 * another. */

import { useState } from "react"

import { Button } from "@/components/ui/button"
import {
  Field,
  FieldDescription,
  FieldGroup,
  FieldLabel,
} from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Spinner } from "@/components/ui/spinner"
import {
  createOwnerContact,
  ownerContactId,
  renameOwnerContact,
  saveOwnerContactSetting,
  type OwnerContact,
} from "@/lib/api/owner-contact"
import { ApiError } from "@/lib/api/types"
import { cn } from "@/lib/utils"

function contactName(owner?: OwnerContact): string {
  const name = owner?.contact?.properties.name
  return typeof name === "string" ? name : ""
}

export function OwnerNameForm({
  repository,
  current,
  layout = "door",
  submitLabel = "Continue",
  secondaryLabel = "Cancel",
  onSaved,
  onSecondary,
}: {
  /** The repository's name, written as the contact's `substrate`. */
  repository: string
  /** The setting and the contact it names, as read. Absent at registration,
   * where nothing has written either. */
  current?: OwnerContact
  /** `door` is the registration card's large controls, stacked; `page` is
   * a settings page's. */
  layout?: "door" | "page"
  submitLabel?: string
  secondaryLabel?: string
  onSaved: (contactId: string) => unknown
  /** Cancel on the settings page: it writes nothing. Registration passes
   * none, because the name step there has no way past it but saving. */
  onSecondary?: () => void
}) {
  const door = layout === "door"
  const [name, setName] = useState(() => contactName(current))
  const [contactId, setContactId] = useState(current?.contact?.id ?? "")
  const [settingNames, setSettingNames] = useState(
    ownerContactId(current?.setting ?? null)
  )
  const [settingExists, setSettingExists] = useState(Boolean(current?.setting))
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const trimmed = name.trim()

  async function save() {
    if (!trimmed || busy) return
    setError(null)
    setBusy(true)
    let id = contactId
    try {
      if (id) {
        await renameOwnerContact(id, trimmed)
      } else {
        id = (await createOwnerContact(trimmed, repository)).id
        setContactId(id)
      }
      if (settingNames !== id) {
        await saveOwnerContactSetting({ contactId: id, exists: settingExists })
        setSettingNames(id)
        setSettingExists(true)
      }
    } catch (err) {
      setError(
        `Saving your name failed: ${err instanceof ApiError ? err.message : "something went wrong"}. Try again.`
      )
      setBusy(false)
      return
    }
    // Outside the try: the name is saved, so a failure from here on is the
    // caller's, not a failed save.
    try {
      await onSaved(id)
    } finally {
      setBusy(false)
    }
  }

  const buttonSize = door ? "lg" : "default"
  return (
    <form
      className={door ? undefined : "max-w-sm"}
      onSubmit={(e) => {
        e.preventDefault()
        void save()
      }}
    >
      <FieldGroup>
        {error && (
          <p
            role="alert"
            className="rounded-lg bg-bad-soft px-3 py-2 text-sm text-destructive"
          >
            {error}
          </p>
        )}
        <Field>
          <FieldLabel htmlFor="owner-name">Your name</FieldLabel>
          <Input
            id="owner-name"
            className={door ? "h-10 text-base" : undefined}
            autoComplete="name"
            autoFocus={door}
            value={name}
            onChange={(e) => setName(e.target.value)}
          />
          <FieldDescription>
            The name on your own contact.
            {door && " You can change it later in Settings."}
          </FieldDescription>
        </Field>
        <Field className={door ? undefined : "flex-row flex-wrap *:w-auto"}>
          <Button
            type="submit"
            size={buttonSize}
            className={cn(door && "h-10")}
            disabled={!trimmed || busy}
          >
            {busy && <Spinner />}
            {submitLabel}
          </Button>
          {onSecondary && (
            <Button
              type="button"
              variant="ghost"
              size={buttonSize}
              className={cn(door && "h-10")}
              disabled={busy}
              onClick={onSecondary}
            >
              {secondaryLabel}
            </Button>
          )}
        </Field>
      </FieldGroup>
    </form>
  )
}
