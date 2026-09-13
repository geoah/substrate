import { useRef, useState, type KeyboardEvent, type MouseEvent } from "react"

import type { SubstrateRecord } from "@/lib/api/types"
import { KitIcon } from "./icons"
import { records } from "./sdk"

export interface QuickAddProps {
  kind: string
  /** The property the typed text lands in. */
  property: string
  defaults?: Record<string, unknown>
  placeholder?: string
  onCreated?(record: SubstrateRecord): void
}

function messageOf(err: unknown): string {
  if (err && typeof err === "object" && "problems" in err) {
    const problems = (err as { problems?: unknown }).problems
    if (Array.isArray(problems) && problems.length) return problems.join("; ")
  }
  return err instanceof Error ? err.message : String(err)
}

/** One input that puts a record. The idempotency key is minted once per
 * pending submit: a failed put keeps its key beside its text, so retrying
 * the same line lands one record however many times the network dropped
 * it, and a new line mints a new key. iOS opens the keyboard only for a
 * focus made inside the tap itself, so the whole bar focuses the input in
 * its own click handler. */
export function QuickAdd({
  kind,
  property,
  defaults,
  placeholder,
  onCreated,
}: QuickAddProps) {
  const input = useRef<HTMLInputElement>(null)
  const [value, setValue] = useState("")
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<string>()
  const attempt = useRef<{ key: string; value: string } | undefined>(undefined)

  const submit = async () => {
    const text = value.trim()
    if (!text || pending) return
    const key =
      attempt.current?.value === text
        ? attempt.current.key
        : crypto.randomUUID()
    attempt.current = { key, value: text }
    setPending(true)
    setError(undefined)
    setValue("")
    try {
      const created = await records.put(
        kind,
        { properties: { ...defaults, [property]: text } },
        { idempotencyKey: key }
      )
      attempt.current = undefined
      onCreated?.(created)
    } catch (err) {
      setError(messageOf(err))
      setValue((current) => current || text)
    } finally {
      setPending(false)
    }
  }

  const focus = (e: MouseEvent) => {
    if (e.target !== input.current) input.current?.focus()
  }

  // Not a <form>: the guest frame is sandboxed without `allow-forms`, where
  // the browser blocks a submission before it dispatches `submit`, so the
  // line is sent from the Enter key and the button instead.
  const onKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key !== "Enter" || e.nativeEvent.isComposing) return
    e.preventDefault()
    void submit()
  }

  return (
    <div className="kit-quickadd-form">
      <div className="kit-quickadd" onClick={focus}>
        <span className="kit-quickadd-plus" aria-hidden>
          <KitIcon name="plus" />
        </span>
        <input
          ref={input}
          className="kit-quickadd-input"
          type="text"
          value={value}
          placeholder={placeholder ?? "Add"}
          aria-label={placeholder ?? "Add"}
          autoComplete="off"
          autoCapitalize="sentences"
          enterKeyHint="done"
          onChange={(e) => setValue(e.target.value)}
          onKeyDown={onKeyDown}
        />
        {value.trim() !== "" && (
          <button
            type="button"
            className="kit-btn kit-quickadd-submit"
            disabled={pending}
            onClick={() => void submit()}
          >
            Add
          </button>
        )}
      </div>
      {error && (
        <p className="kit-quickadd-error" role="alert">
          {error}
        </p>
      )}
    </div>
  )
}
