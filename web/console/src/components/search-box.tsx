/** The one text-search control: a query in the search grammar (`lay*` a word
 * prefix, `"a phrase"`, `-word`, `a OR b`), applied a beat after typing stops
 * or on Enter; Escape and the × clear it. The value the caller holds (the
 * URL, on both pages that use this) is the truth the box mirrors, so a
 * restored or shared view fills it in, while a value the box itself just sent
 * never overwrites what has been typed since. */

import { useEffect, useRef, useState } from "react"
import { SearchIcon, XIcon } from "lucide-react"

import {
  InputGroup,
  InputGroupAddon,
  InputGroupButton,
  InputGroupInput,
} from "@/components/ui/input-group"

/** How long typing pauses before the query is sent. */
const SETTLE_MS = 350

export function SearchBox({
  value,
  onChange,
  label,
  placeholder,
  className,
  autoFocus,
}: {
  value: string
  onChange: (next: string) => void
  /** The accessible name; also the placeholder unless one is given. */
  label: string
  placeholder?: string
  className?: string
  autoFocus?: boolean
}) {
  const [draft, setDraft] = useState(value)
  const applied = useRef(value)
  // The latest onChange, so the settle timer never calls a stale closure and
  // a parent re-render never re-arms it.
  const emit = useRef(onChange)
  useEffect(() => {
    emit.current = onChange
  }, [onChange])
  function apply(next: string) {
    applied.current = next
    emit.current(next)
  }
  useEffect(() => {
    if (value !== applied.current) {
      applied.current = value
      setDraft(value)
    }
  }, [value])
  useEffect(() => {
    if (draft === applied.current) return
    const id = window.setTimeout(() => apply(draft), SETTLE_MS)
    return () => window.clearTimeout(id)
  }, [draft])
  return (
    <InputGroup className={className}>
      <InputGroupAddon>
        <SearchIcon />
      </InputGroupAddon>
      <InputGroupInput
        aria-label={label}
        placeholder={placeholder ?? label}
        className="data"
        autoFocus={autoFocus}
        value={draft}
        onChange={(e) => setDraft(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter") apply(draft)
          if (e.key === "Escape") {
            setDraft("")
            apply("")
          }
        }}
      />
      {draft && (
        <InputGroupAddon align="inline-end">
          <InputGroupButton
            aria-label="Clear search"
            size="icon-xs"
            onClick={() => {
              setDraft("")
              apply("")
            }}
          >
            <XIcon />
          </InputGroupButton>
        </InputGroupAddon>
      )}
    </InputGroup>
  )
}
