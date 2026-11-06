/** A key for one AI model row, typed in place on its Providers card: the
 * row's `apiKey`, written through the record page's own secret write
 * (`useRecordPatch`, one property under `ifVersion`), so it is sealed the same
 * way and never read back. The words say what the key changes: switching
 * from the operator's key, replacing your own, or adding the first. */

import { useState } from "react"
import { useQueryClient } from "@tanstack/react-query"

import {
  useRecordPatch,
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
      return `From now on your agents call ${name} with your key, billed to your account, and those calls no longer count toward ${words.possessive} allowance. The key is sealed on this card’s record and never shown again.`
    case "replace":
      return "The new key replaces the one sealed on this card’s record. It is never shown again."
    case "add":
      return `Your agents call ${name} with this key, billed to your account. It is sealed on this card’s record and never shown again.`
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
  const patch = useRecordPatch(row)
  const client = useQueryClient()
  const id = `model-key-${row.id}`
  return (
    <form
      className="relative z-10 rounded-lg border border-border bg-panel p-3"
      onSubmit={(e) => {
        e.preventDefault()
        const value = key.trim()
        if (!value) return
        patch.mutate(
          { apiKey: value },
          {
            onSuccess: () => {
              void client.invalidateQueries({
                queryKey: operatorDefaultsQueryOptions.queryKey,
              })
              onDone()
            },
          }
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
      {patch.error && (
        <p role="alert" className="mt-2 text-[12.5px] text-destructive">
          {writeError(patch.error)}
        </p>
      )}
      <div className="mt-2.5 flex flex-wrap gap-2">
        <Button
          size="sm"
          type="submit"
          disabled={!key.trim() || patch.isPending}
        >
          {patch.isPending && <Spinner className="size-3" />}
          Save key
        </Button>
        <Button
          variant="ghost"
          size="sm"
          type="button"
          disabled={patch.isPending}
          onClick={onDone}
        >
          Cancel
        </Button>
      </div>
    </form>
  )
}
