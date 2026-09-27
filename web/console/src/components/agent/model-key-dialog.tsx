/** "Add your OpenAI key": the one write an agent needs before it can answer,
 * offered where it refuses for want of one. The key is the llm/provider row's
 * `apiKey`, written through the record page's own secret write
 * (`useRecordPatch`, one property under `ifVersion`), so it is sealed the same
 * way and never read back. */

import { useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import { KeyRoundIcon } from "lucide-react"

import {
  useRecordPatch,
  writeError,
} from "@/components/property-sheet/use-record-patch"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Spinner } from "@/components/ui/spinner"
import { providerName } from "@/lib/agent-chat"
import { providerEndpoint } from "@/lib/api/agents"
import { CORE_AUTHORITY, LLM_PACKAGE_NAME } from "@/lib/api/http"
import { recordQueryOptions } from "@/lib/api/records"
import type { SubstrateRecord } from "@/lib/api/types"

/** The provider row's record page. */
function ProviderLink({ id, children }: { id: string; children: string }) {
  return (
    <Link
      to="/data/$authority/$pkg/$name/$id"
      params={{
        authority: CORE_AUTHORITY,
        pkg: LLM_PACKAGE_NAME,
        name: "provider",
        id,
      }}
      className="text-primary-text underline underline-offset-2"
    >
      {children}
    </Link>
  )
}

function KeyForm({
  provider,
  onDone,
}: {
  provider: SubstrateRecord
  onDone: () => void
}) {
  const [key, setKey] = useState("")
  const patch = useRecordPatch(provider)
  const name = providerName(provider.id)
  const endpointMissing = providerEndpoint(provider) === "missing baseURL"
  return (
    <form
      className="flex flex-col gap-3"
      onSubmit={(e) => {
        e.preventDefault()
        const value = key.trim()
        if (!value) return
        patch.mutate({ apiKey: value }, { onSuccess: onDone })
      }}
    >
      <label htmlFor="model-key" className="text-[13px] font-medium">
        {name} API key
      </label>
      <Input
        id="model-key"
        type="password"
        autoComplete="off"
        spellCheck={false}
        autoFocus
        value={key}
        onChange={(e) => setKey(e.target.value)}
        className="font-mono"
      />
      <p className="text-[12.5px] text-muted-foreground">
        It’s sealed on the {name} provider’s record: agents use it, and nobody
        can read it back here. Paste a new one to replace it.
      </p>
      {endpointMissing && (
        <p className="text-[12.5px] text-warning">
          This provider has no endpoint yet, so it also needs one.{" "}
          <ProviderLink id={provider.id}>Set it on its record</ProviderLink>.
        </p>
      )}
      {patch.error && (
        <p role="alert" className="text-[12.5px] text-destructive">
          {writeError(patch.error)}
        </p>
      )}
      <DialogFooter>
        <Button
          type="button"
          variant="outline"
          disabled={patch.isPending}
          onClick={onDone}
        >
          Cancel
        </Button>
        <Button type="submit" disabled={!key.trim() || patch.isPending}>
          {patch.isPending && <Spinner className="size-3" />}
          Save the key
        </Button>
      </DialogFooter>
    </form>
  )
}

/** The dialog, reading the provider row once it opens. */
export function ModelKeyDialog({
  providerId,
  open,
  onOpenChange,
}: {
  /** The llm/provider row's id (`openai`). */
  providerId: string
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const row = useQuery({
    ...recordQueryOptions(
      CORE_AUTHORITY,
      LLM_PACKAGE_NAME,
      "provider",
      providerId
    ),
    enabled: open,
  })
  const name = providerName(providerId)
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <KeyRoundIcon className="size-4 text-muted-foreground" />
            Add your {name} key
          </DialogTitle>
          <DialogDescription>
            Agents run on your own key, so they can’t answer until {name} has
            one.
          </DialogDescription>
        </DialogHeader>
        {row.data ? (
          <KeyForm provider={row.data} onDone={() => onOpenChange(false)} />
        ) : row.isError ? (
          <p role="alert" className="text-[13px] text-destructive">
            The {name} provider didn’t load: {row.error.message}
          </p>
        ) : (
          <p className="flex items-center gap-1.5 text-[13px] text-muted-foreground">
            <Spinner className="size-3" />
            Loading
          </p>
        )}
      </DialogContent>
    </Dialog>
  )
}

/** A button that opens the dialog: the words say which key. */
export function AddKeyButton({
  providerId,
  size = "sm",
  variant = "outline",
}: {
  providerId: string
  size?: "sm" | "xs"
  variant?: "outline" | "default" | "ghost"
}) {
  const [open, setOpen] = useState(false)
  return (
    <>
      <Button size={size} variant={variant} onClick={() => setOpen(true)}>
        <KeyRoundIcon />
        Add your {providerName(providerId)} key
      </Button>
      <ModelKeyDialog
        providerId={providerId}
        open={open}
        onOpenChange={setOpen}
      />
    </>
  )
}
