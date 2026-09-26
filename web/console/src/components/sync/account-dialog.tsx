/** The Add account dialog: where a person creates a provider account, written
 * for someone who has never seen a substrate. It asks only what the OWNER
 * decides — which streams to sync and on what schedule — off the account
 * kind's declaration (`lib/account-form`), says in numbered steps what
 * happens next, and on an OAuth provider whose credentials are set the one
 * primary button CREATES the record and opens the provider's consent, the
 * tab opened synchronously from the press so the async create does not hand
 * it to the popup blocker. The same dialog EDITS an account's toggles,
 * cadence and depth, without the connect step. */

import { useMemo, useState } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { KeyRoundIcon, TriangleAlertIcon } from "lucide-react"

import { PropertyField } from "@/components/record/property-field"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import {
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLegend,
  FieldSet,
} from "@/components/ui/field"
import { Spinner } from "@/components/ui/spinner"
import { toast } from "@/components/ui/toast"
import type { OAuthConnect } from "@/hooks/use-oauth-connect"
import { kindsQueryOptions } from "@/lib/api/kinds"
import { createRecord, patchRecord } from "@/lib/api/records"
import type { KindInfo, SubstrateRecord } from "@/lib/api/types"
import { accountFormGroups } from "@/lib/account-form"
import { splitKind } from "@/lib/definition"
import { recordPath } from "@/lib/record-path"
import {
  initialValues,
  toProperties,
  validate,
  type FieldError as FieldErr,
  type FormMode,
  type FormValue,
  type FormValues,
} from "@/lib/record-form"

export interface AccountDialogProps {
  /** The provider's `accountconfig` kind. */
  kind: KindInfo
  /** The provider's own name (`google`), as the copy addresses it. */
  providerName: string
  /** The provider connects through the host's consent flow (its client kind
   * wears `oauth2`). A token provider syncs as soon as the account exists. */
  oauth: boolean
  /** The provider's credentials are set, so a create can go on to consent. */
  configured: boolean
  /** The connect mutation the OWNING surface holds, so the return listener
   * outlives this dialog, which closes as the consent tab opens. Absent on a
   * token provider. */
  connect?: OAuthConnect
  /** Opens the credentials form; offered while the credentials are missing. */
  onSetUpCredentials?: () => void
  /** The account being edited; absent creates one. */
  record?: SubstrateRecord
  /** What the edited account is called in the title. */
  label?: string
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function AccountDialog({
  kind,
  providerName,
  oauth,
  configured,
  connect,
  onSetUpCredentials,
  record,
  label,
  open,
  onOpenChange,
}: AccountDialogProps) {
  const queryClient = useQueryClient()
  const registry = useQuery(kindsQueryOptions)
  const groups = useMemo(() => accountFormGroups(kind), [kind])
  const mode: FormMode = record ? "patch" : "create"
  const seed = useMemo(
    () => initialValues(groups.all, record),
    [groups, record]
  )
  const [values, setValues] = useState<FormValues>(seed)
  const [errors, setErrors] = useState<Record<string, string>>({})
  const [formError, setFormError] = useState<string | undefined>()
  // Reseed whenever the dialog opens or the record changes.
  const seedKey = `${record?.id ?? "new"}:${record?.version ?? ""}:${open}`
  const [seeded, setSeeded] = useState(seedKey)
  if (seeded !== seedKey) {
    setSeeded(seedKey)
    setValues(seed)
    setErrors({})
    setFormError(undefined)
  }

  const { authority, pkg } = splitKind(kind.identity)
  // Create AND connect in one press: an OAuth provider, its credentials set,
  // and a surface that handed in the connect to run.
  const connecting =
    mode === "create" && oauth && configured && Boolean(connect)
  const accountName = `${providerName} account`

  const save = useMutation({
    mutationFn: async ({ tab }: { tab?: Window | null }) => {
      const properties = toProperties(groups.all, values, mode)
      try {
        return record
          ? await patchRecord(authority, pkg, kind.name, record.id, {
              properties,
            })
          : await createRecord(authority, pkg, kind.name, { properties })
      } catch (error) {
        tab?.close()
        throw error
      }
    },
    onSuccess: (saved, { tab }) => {
      // An account write moves the bundle status, the trait query and the
      // record reads all at once.
      void queryClient.invalidateQueries()
      onOpenChange(false)
      if (record) {
        toast.add({ type: "success", title: "Account updated" })
        return
      }
      if (connecting && connect) {
        connect.mutate({
          record: recordPath(saved.kind, saved.id),
          label: accountName,
          tab,
        })
        return
      }
      toast.add({
        type: "success",
        title: "Account created",
        description: oauth
          ? "Press Connect beside it to approve it with the provider."
          : "Its first sync starts on its own.",
      })
    },
    onError: (error) => {
      toast.add({
        type: "error",
        title: record
          ? "Saving the account failed"
          : "Creating the account failed",
        description: error.message,
      })
    },
  })

  function setValue(field: string, value: FormValue) {
    setValues((prev) => ({ ...prev, [field]: value }))
    setFormError(undefined)
    setErrors((prev) => {
      if (!prev[field]) return prev
      const next = { ...prev }
      delete next[field]
      return next
    })
  }

  function submit() {
    const failures = validate(groups.all, values, mode)
    if (failures.length) {
      setErrors(
        Object.fromEntries(failures.map((f: FieldErr) => [f.name, f.message]))
      )
      return
    }
    // An account with nothing turned on requests no scope and syncs nothing:
    // a first-time creator is told, rather than left with a silent row. An
    // edit may switch everything off, which is how a sync is stopped for good.
    if (
      mode === "create" &&
      groups.toggles.length > 0 &&
      !groups.toggles.some((f) => values[f.name] === true)
    ) {
      setFormError("Turn on at least one thing to bring in.")
      return
    }
    // The consent tab is opened HERE, from the press, before the create's
    // round-trip: opened after it, the browser counts it as a popup.
    const tab = connecting ? window.open("about:blank", "_blank") : undefined
    save.mutate({ tab })
  }

  const title = record
    ? `Edit ${label ?? accountName}`
    : `Add a ${providerName} account`
  const description = record
    ? "Change what this account brings in, how often, and how far back. Its sign-in stays as it is."
    : oauth
      ? configured
        ? `Choose what to bring in. ${providerName} then opens in a new tab and asks you to approve access to each thing you turned on. It starts once you approve.`
        : `Choose what to bring in. This account cannot connect until ${providerName}’s sign-in details are added.`
      : configured
        ? `Choose what to bring in. The account uses ${providerName}’s sign-in details, and its first sync starts on its own.`
        : `Choose what to bring in. This account cannot sync until ${providerName}’s sign-in details are added.`

  const field = (f: (typeof groups.all)[number]) => (
    <PropertyField
      key={f.name}
      field={f}
      value={values[f.name]}
      onChange={(next) => setValue(f.name, next)}
      mode={mode}
      error={errors[f.name]}
      kinds={registry.data ?? []}
      idPrefix="account"
    />
  )

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => !save.isPending && onOpenChange(next)}
    >
      <DialogContent className="sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>{description}</DialogDescription>
        </DialogHeader>

        {!record && (
          <ol className="flex flex-col gap-1.5 text-xs text-muted-foreground">
            <Step n={1}>Choose what to bring in and how often, below.</Step>
            {oauth ? (
              <>
                <Step n={2}>
                  Approve access with {providerName} in the tab that opens.
                </Step>
                <Step n={3}>
                  Come back here. The first sync starts on its own.
                </Step>
              </>
            ) : (
              <Step n={2}>The first sync starts on its own.</Step>
            )}
          </ol>
        )}

        {!record && !configured && (
          <div
            role="note"
            className="flex items-start gap-2 rounded-md border border-warning/40 px-3 py-2 text-xs"
          >
            <TriangleAlertIcon className="mt-0.5 size-3.5 shrink-0 text-warning" />
            <div className="flex flex-col gap-1.5">
              <p>
                {providerName}’s sign-in details are not added yet, so this
                account cannot {oauth ? "connect" : "sync"} until they are.
                {!onSetUpCredentials && " Add them in step 2 first."}
              </p>
              {onSetUpCredentials && (
                <Button
                  variant="outline"
                  size="sm"
                  className="h-7 w-fit gap-1 px-2 text-xs"
                  onClick={onSetUpCredentials}
                >
                  <KeyRoundIcon className="size-3" />
                  Add sign-in details
                </Button>
              )}
            </div>
          </div>
        )}

        <form
          id="account-dialog-form"
          className="max-h-[55vh] overflow-y-auto px-px"
          onSubmit={(e) => {
            e.preventDefault()
            submit()
          }}
        >
          <FieldGroup className="gap-6">
            {groups.toggles.length > 0 && (
              <FieldSet className="gap-3">
                <FieldLegend variant="label">What to bring in</FieldLegend>
                <FieldDescription>
                  {oauth
                    ? `Each is a permission ${providerName} asks you to approve. Turn on only what you want copied here.`
                    : "Turn on what you want copied here."}
                </FieldDescription>
                {groups.toggles.map(field)}
              </FieldSet>
            )}
            {groups.settings.length > 0 && (
              <FieldSet className="gap-4">
                <FieldLegend variant="label">Settings</FieldLegend>
                {groups.settings.map(field)}
              </FieldSet>
            )}
            {groups.all.length === 0 && (
              <p className="text-xs text-muted-foreground">
                There is nothing to choose for this account: it syncs everything
                the provider allows.
              </p>
            )}
          </FieldGroup>
          {formError && <FieldError className="mt-3">{formError}</FieldError>}
        </form>

        <DialogFooter>
          <Button
            variant="outline"
            disabled={save.isPending}
            onClick={() => onOpenChange(false)}
          >
            Cancel
          </Button>
          <Button
            type="submit"
            form="account-dialog-form"
            disabled={save.isPending}
          >
            {save.isPending && <Spinner className="size-3.5" />}
            {record
              ? "Save changes"
              : connecting
                ? "Create and connect"
                : "Create"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function Step({ n, children }: { n: number; children: React.ReactNode }) {
  return (
    <li className="flex items-start gap-2">
      <span className="mt-px inline-flex size-4 shrink-0 items-center justify-center rounded-full border text-[11.5px] font-medium text-foreground">
        {n}
      </span>
      <span>{children}</span>
    </li>
  )
}
