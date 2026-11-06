/** A key for one AI model row, typed in place on its Providers card: the
 * row's `apiKey`, written as one property under the version the row had when
 * the form opened (`useEditBase`), so a key someone else wrote meanwhile is
 * refused as a conflict instead of overwritten. The write is the secret one
 * (`useSecretPatch`): the typed key is cleared from the form as it is sent,
 * and nothing of it stays in the mutation cache, whether the write succeeds
 * or fails. The words say what the key changes: switching from the
 * operator's key, replacing your own, or adding the first. */

import { useState } from "react"
import { useQueryClient } from "@tanstack/react-query"

import {
  useEditBase,
  useSecretPatch,
  writeError,
} from "@/components/property-sheet/use-record-patch"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Spinner } from "@/components/ui/spinner"
import { operatorDefaultsQueryOptions } from "@/lib/api/operator"
import type { SubstrateRecord } from "@/lib/api/types"
import type { OperatorWords } from "@/lib/operator-defaults"

export type KeyFormMode = "switch" | "replace" | "add"

function consequence(mode: KeyFormMode, name: string, words: OperatorWords) {
  switch (mode) {
    case "switch":
      return `Your agents will call ${name} with this key, billed to your ${name} account, and those calls stop counting toward ${words.possessive} allowance. The key is stored sealed and never shown again.`
    case "replace":
      return `The new key replaces the one stored for ${name}. It is never shown again.`
    case "add":
      return `Your agents will call ${name} with this key, billed to your ${name} account. It is stored sealed and never shown again.`
  }
}

export function ModelKeyForm({
  row,
  name,
  words,
  mode,
  onDone,
}: {
  /** The llm/provider row the key is written to. */
  row: SubstrateRecord
  /** The card's name: "OpenAI". */
  name: string
  words: OperatorWords
  mode: KeyFormMode
  onDone: () => void
}) {
  const [key, setKey] = useState("")
  const write = useSecretPatch(row, "apiKey", useEditBase(row))
  const client = useQueryClient()
  const id = `model-key-${row.id}`
  return (
    <form
      className="relative z-10 rounded-lg border border-border bg-panel p-3"
      onSubmit={(e) => {
        e.preventDefault()
        const value = key.trim()
        if (!value) return
        setKey("")
        write.save(value).then(
          () => {
            void client.invalidateQueries({
              queryKey: operatorDefaultsQueryOptions.queryKey,
            })
            onDone()
          },
          // The refusal shows below the field, which is empty again.
          () => {}
        )
      }}
    >
      <label className="text-[12.5px] font-medium" htmlFor={id}>
        Your {name} API key
      </label>
      <Input
        id={id}
        type="password"
        autoComplete="off"
        spellCheck={false}
        autoFocus
        value={key}
        onChange={(e) => setKey(e.target.value)}
        className="mt-1.5 font-mono"
      />
      <p className="mt-2 text-[12px] text-faint">
        {consequence(mode, name, words)}
      </p>
      {write.error && (
        <p role="alert" className="mt-2 text-[12.5px] text-destructive">
          {writeError(write.error)}
        </p>
      )}
      <div className="mt-2.5 flex flex-wrap gap-2">
        <Button size="sm" type="submit" disabled={!key.trim() || write.pending}>
          {write.pending && <Spinner className="size-3" />}
          Save key
        </Button>
        <Button
          variant="ghost"
          size="sm"
          type="button"
          disabled={write.pending}
          onClick={onDone}
        >
          Cancel
        </Button>
      </div>
    </form>
  )
}
