/** The record page's Sync tab, for any kind binding the core `sync` trait:
 * the generic renderer over the record's own properties, with the two owner
 * actions and a link to the Connection's operational page. */

import { useMemo } from "react"
import { useQuery } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import { ArrowUpRightIcon } from "lucide-react"

import { SyncActions, SyncSummary } from "@/components/sync/sync-panel"
import { Button } from "@/components/ui/button"
import { splitKind } from "@/lib/api/http"
import { triggerRecordsQueryOptions } from "@/lib/api/sync"
import type { SubstrateRecord } from "@/lib/api/types"
import { requestTriggers, syncFieldsOf, triggersOnKind } from "@/lib/sync"

export function SyncRail({ record }: { record: SubstrateRecord }) {
  const triggers = useQuery(triggerRecordsQueryOptions)
  const fields = useMemo(
    () => syncFieldsOf(record.properties),
    [record.properties]
  )
  const requestIds = useMemo(
    () =>
      requestTriggers(triggersOnKind(triggers.data ?? [], record.kind)).map(
        (s) => s.id
      ),
    [triggers.data, record.kind]
  )
  const parts = splitKind(record.kind)
  const legacy =
    typeof record.properties.syncStatus === "string"
      ? record.properties.syncStatus
      : undefined
  return (
    <div className="flex flex-col gap-4 p-4">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <SyncActions
          record={record}
          paused={fields.paused}
          requestTriggerIds={requestIds}
        />
        <Button
          variant="ghost"
          size="sm"
          className="gap-1"
          render={
            <Link
              to="/connections/$authority/$pkg/$name/$id"
              params={{ ...parts, id: record.id }}
            />
          }
        >
          Open in Connections
          <ArrowUpRightIcon className="size-3.5" />
        </Button>
      </div>
      <SyncSummary fields={fields} legacyStatus={legacy} />
    </div>
  )
}
