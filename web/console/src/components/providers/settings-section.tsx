/** A bundle's own settings, and what else it still needs. SETTINGS: its
 * `setting` and `secret` records as one form (decision record 0076), a
 * secret write-only. NEEDS: every setup item the four steps and the form do
 * not already cover, in the server's words, linking the record that would
 * clear it. INPUTS (technical mode only): which record each declared need
 * uses, with a picker to bind another. */

import { useState } from "react"
import { useMutation, useQuery } from "@tanstack/react-query"
import { useQueryClient } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import { ArrowUpRightIcon, TriangleAlertIcon } from "lucide-react"

import { BundleSettingsForm } from "@/components/bundle-settings"
import { IdText } from "@/components/identity/id-text"
import { KindRef } from "@/components/identity/kind-ref"
import { RecordConfigForm } from "@/components/record-config-form"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { toast } from "@/components/ui/toast"
import {
  bindBundleInput,
  refetchBundleStateSoon,
  seedBundleStatus,
} from "@/lib/api/bundles"
import { LLM_PACKAGE, LLM_PACKAGE_NAME, CORE_AUTHORITY } from "@/lib/api/http"
import { recordsQueryOptions } from "@/lib/api/records"
import type {
  BundleStatus,
  InputStatus,
  KindInfo,
  SetupItem,
  SubstrateRecord,
} from "@/lib/api/types"
import { isInputSetupCode } from "@/lib/bundles"
import { kindByIdentity, splitKind } from "@/lib/definition"
import { recordTitle } from "@/lib/format"
import { kindHasTrait, OAUTH2_CLIENT_PROPERTIES } from "@/lib/sync"
import type { SettingField } from "@/lib/settings"
import { untitled } from "@/lib/kind-names"

export function SettingsForm({ fields }: { fields: SettingField[] }) {
  return (
    <div className="rounded-[10px] border px-4 py-4">
      <BundleSettingsForm fields={fields} />
    </div>
  )
}

/** One setup item that is not an input's own resolution problem nor a
 * setting the form marks: an incomplete OAuth client, or an agent's
 * llm/provider row absent or keyless. */
export function SetupItemRow({
  item,
  kinds,
}: {
  item: SetupItem
  kinds: KindInfo[]
}) {
  // "provider" always means the llm package's own provider kind.
  const kind =
    item.code === "provider"
      ? kindByIdentity(kinds, `${LLM_PACKAGE}/provider`)
      : item.kind
        ? kindByIdentity(kinds, item.kind)
        : undefined
  const authority = item.code === "provider" ? CORE_AUTHORITY : kind?.authority
  const pkg = item.code === "provider" ? LLM_PACKAGE_NAME : kind?.package
  const name = item.code === "provider" ? "provider" : kind?.name
  return (
    <div
      data-slot="setup-item"
      className="flex items-start justify-between gap-3 rounded-lg bg-warn-soft px-3.5 py-2.5 text-[13px]"
    >
      <p className="flex min-w-0 items-start gap-2">
        <TriangleAlertIcon
          aria-hidden
          className="mt-0.5 size-4 shrink-0 text-warning"
        />
        <span className="min-w-0 [overflow-wrap:anywhere]">{item.message}</span>
      </p>
      {item.record && authority && pkg && name && (
        <Link
          to="/data/$authority/$pkg/$name/$id"
          params={{ authority, pkg, name, id: item.record }}
          className="inline-flex shrink-0 items-center gap-0.5 text-primary-text underline-offset-2 hover:underline"
        >
          Open
          <ArrowUpRightIcon aria-hidden className="size-3.5" />
        </Link>
      )}
    </div>
  )
}

/** One declared input: which record it uses and how that record was chosen,
 * or what is wrong in the server's words, with the records of its kind to
 * bind and a form to create the first one. */
export function InputCard({
  bundle,
  input,
  kinds,
}: {
  bundle: BundleStatus
  input: InputStatus
  kinds: KindInfo[]
}) {
  const queryClient = useQueryClient()
  const kind = kindByIdentity(kinds, input.kind)
  const declared = splitKind(input.kind)
  const authority = kind?.authority ?? declared.authority
  const pkg = kind?.package ?? declared.pkg
  const [editing, setEditing] = useState<SubstrateRecord | "new" | null>(null)
  const records = useQuery({
    ...recordsQueryOptions({
      authority,
      package: pkg,
      name: kind?.name ?? "",
      first: 50,
    }),
    enabled: Boolean(kind),
  })
  const rows = records.data?.records ?? []
  const problem = bundle.setup?.find(
    (item) => item.input === input.name && isInputSetupCode(item.code)
  )
  const bind = useMutation({
    mutationFn: (record: string) =>
      bindBundleInput(bundle.id, input.name, record),
    onSuccess: (status, record) => {
      toast.add({
        type: "success",
        title: record
          ? `${input.name} now uses that record.`
          : `${input.name} unbound.`,
      })
      seedBundleStatus(queryClient, status)
      refetchBundleStateSoon(queryClient)
    },
    onError: (error, record) => {
      toast.add({
        type: "error",
        title: record
          ? `Binding ${input.name} failed`
          : `Unbinding ${input.name} failed`,
        description: error.message,
      })
    },
  })
  const VIA = {
    bound: "chosen by you",
    default: "the one named default",
    sole: "the only one there is",
  } as const
  return (
    <div data-slot="input-card" className="rounded-[10px] border">
      <div className="flex flex-wrap items-start justify-between gap-x-3 gap-y-1 border-b px-3.5 py-2.5">
        <div className="min-w-0">
          <div className="font-medium">{input.name}</div>
          {input.description && (
            <p className="text-[12.5px] text-muted-foreground">
              {input.description}
            </p>
          )}
          <div className="mt-0.5">
            <KindRef
              kind={kind ?? input.kind}
              mode="reference"
              link={Boolean(kind)}
            />
          </div>
        </div>
        <div className="min-w-0 text-right text-[12.5px]">
          {input.record ? (
            <span className="text-muted-foreground">
              Uses <IdText value={input.record} />
              {input.via ? `, ${VIA[input.via]}` : ""}
            </span>
          ) : (
            <span className="inline-flex items-center gap-1.5 text-warning">
              <TriangleAlertIcon aria-hidden className="size-3.5 shrink-0" />
              {problem?.message ?? "Nothing chosen yet."}
            </span>
          )}
        </div>
      </div>
      {!kind ? (
        <p className="px-3.5 py-2.5 text-[12.5px] text-muted-foreground">
          Its collection isn’t in this repository yet, so there is nothing to
          choose from.
        </p>
      ) : records.isPending ? (
        <Skeleton className="m-3 h-12 rounded-md" />
      ) : records.isError ? (
        <p className="px-3.5 py-2.5 text-[12.5px] text-muted-foreground">
          The records didn’t load: {records.error.message}
        </p>
      ) : rows.length === 0 ? (
        <p className="px-3.5 py-2.5 text-[12.5px] text-muted-foreground">
          There is none yet. Create one and it is used on its own.
        </p>
      ) : (
        <div className="divide-y">
          {rows.map((record) => {
            const boundHere =
              input.via === "bound" && input.record === record.id
            return (
              <div
                key={record.id}
                className="flex items-center justify-between gap-3 px-3.5 py-2"
              >
                <div className="min-w-0">
                  <div className="truncate">
                    {recordTitle(record.properties) || untitled(kind)}
                  </div>
                  <IdText value={record.id} />
                </div>
                <div className="flex shrink-0 items-center gap-1.5">
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => setEditing(record)}
                  >
                    Edit
                  </Button>
                  <Button
                    variant="outline"
                    size="sm"
                    disabled={bind.isPending}
                    onClick={() => bind.mutate(boundHere ? "" : record.id)}
                  >
                    {boundHere ? "Unbind" : "Use this"}
                  </Button>
                </div>
              </div>
            )
          })}
        </div>
      )}
      {kind && (
        <div className="flex justify-end border-t px-3.5 py-2">
          <Button variant="outline" size="sm" onClick={() => setEditing("new")}>
            Create one
          </Button>
        </div>
      )}
      {kind && editing && (
        <RecordConfigForm
          type={kind}
          first={
            kindHasTrait(kind, "oauth2") ? OAUTH2_CLIENT_PROPERTIES : undefined
          }
          record={editing === "new" ? undefined : editing}
          open={Boolean(editing)}
          onOpenChange={(open) => !open && setEditing(null)}
          title={
            editing === "new"
              ? `Create ${untitled(kind).replace(/^Untitled /, "a ")}`
              : `Edit ${recordTitle(editing.properties) || untitled(kind)}`
          }
          description="A secret never shows again once saved; leave it blank to keep the one you saved."
        />
      )}
    </div>
  )
}
