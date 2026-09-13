/** The picker an app shows when an input is ambiguous (two or more records
 * of the input's kind, no binding and no `default`), and the settings sheet's
 * control for every input. A pick writes `bindings.<name>` on the app record
 * itself, the owner's choice surviving a re-apply because apply merges. The
 * current pick, when there is one, is marked. */

import { useState } from "react"
import { useQueryClient } from "@tanstack/react-query"
import { CircleDotIcon } from "lucide-react"

import { Button } from "@/components/ui/button"
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import { Spinner } from "@/components/ui/spinner"
import { toast } from "@/components/ui/toast"
import { APP_NAME } from "@/lib/api/apps"
import { CORE_AUTHORITY, CORE_PACKAGE_NAME } from "@/lib/api/http"
import { patchRecord } from "@/lib/api/records"
import type { KindInfo, SubstrateRecord } from "@/lib/api/types"
import type { InputSpec } from "@/lib/apps/spec"
import { recordTitle } from "@/lib/format"
import { recordPath } from "@/lib/record-path"

/** A record's heading as an option: the server title, else its `name`, else
 * its id. */
function titleOf(record: SubstrateRecord): string {
  const title = recordTitle(record.properties)
  if (title) return title
  const name = record.properties.name
  return typeof name === "string" && name ? name : record.id
}

export function InputBinder({
  app,
  name,
  input,
  kind,
  options,
  current,
}: {
  app: SubstrateRecord
  name: string
  input: InputSpec
  kind?: KindInfo
  options: SubstrateRecord[]
  /** The record the input resolves to today, marked in the list. */
  current?: SubstrateRecord
}) {
  const queryClient = useQueryClient()
  const [picking, setPicking] = useState<string | null>(null)

  async function bind(option: SubstrateRecord) {
    setPicking(option.id)
    const existing =
      app.properties.bindings &&
      typeof app.properties.bindings === "object" &&
      !Array.isArray(app.properties.bindings)
        ? (app.properties.bindings as Record<string, unknown>)
        : {}
    try {
      await patchRecord(CORE_AUTHORITY, CORE_PACKAGE_NAME, APP_NAME, app.id, {
        properties: {
          bindings: {
            ...existing,
            [name]: { ref: recordPath(option.kind, option.id) },
          },
        },
        ifVersion: app.version,
      })
      toast.add({
        type: "success",
        title: `${name} is ${titleOf(option)}`,
      })
      void queryClient.invalidateQueries({
        queryKey: ["record", CORE_AUTHORITY, CORE_PACKAGE_NAME, APP_NAME],
      })
      void queryClient.invalidateQueries({
        queryKey: ["records", CORE_AUTHORITY, CORE_PACKAGE_NAME, APP_NAME],
      })
    } catch (error) {
      toast.add({
        type: "error",
        title: `Could not bind ${name}`,
        description: error instanceof Error ? error.message : String(error),
      })
    } finally {
      setPicking(null)
    }
  }

  return (
    <Empty className="py-10">
      <EmptyHeader>
        <EmptyMedia variant="icon">
          <CircleDotIcon />
        </EmptyMedia>
        <EmptyTitle>
          Which {kind?.name ?? "record"} is `{name}`?
        </EmptyTitle>
        <EmptyDescription>
          {input.description ??
            `This app reads one ${kind?.name ?? "record"} as ${name}, and ${options.length} qualify.`}
        </EmptyDescription>
      </EmptyHeader>
      <EmptyContent>
        <ul className="flex w-full max-w-sm flex-col gap-2">
          {options.map((option) => (
            <li key={option.id}>
              <Button
                variant="outline"
                className="h-11 w-full justify-between gap-3 px-3"
                disabled={picking !== null}
                onClick={() => void bind(option)}
              >
                <span className="truncate">
                  {titleOf(option)}
                  {current?.id === option.id && (
                    <span className="ml-2 text-xs text-muted-foreground">
                      current
                    </span>
                  )}
                </span>
                {picking === option.id ? (
                  <Spinner className="size-3.5" />
                ) : (
                  <span className="data text-xs text-muted-foreground">
                    {option.id}
                  </span>
                )}
              </Button>
            </li>
          ))}
        </ul>
      </EmptyContent>
    </Empty>
  )
}
