/** The picker an app shows when an input is ambiguous (two or more records
 * of the input's kind, no binding and no `default`) or missing, and the
 * settings sheet's control for every input. A pick writes `bindings.<name>`
 * on the app record itself, the owner's choice surviving a re-apply because
 * apply merges. The current pick, when there is one, is marked.
 *
 * The picker pages the collection ON ITS OWN, page by page behind Load more:
 * the resolver (`lib/apps/inputs.ts`) reads by id and never holds a page of
 * candidates, so nothing here is what the guest was told, and a collection
 * larger than one page is walked rather than cut. */

import { useState } from "react"
import { useInfiniteQuery, useQueryClient } from "@tanstack/react-query"
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
import { patchRecord, recordsInfiniteOptions } from "@/lib/api/records"
import type { KindInfo, SubstrateRecord } from "@/lib/api/types"
import type { InputSpec } from "@/lib/apps/spec"
import { splitKind } from "@/lib/definition"
import { recordTitle } from "@/lib/format"
import { recordPath } from "@/lib/record-path"

/** One page of candidates: a thumb's worth, walked further on request. */
const PICKER_PAGE = 50

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
  current,
  status,
}: {
  app: SubstrateRecord
  name: string
  input: InputSpec
  /** The input's kind; absent when the repository does not have it, and
   * then there is nothing to list. */
  kind?: KindInfo
  /** The record the input resolves to today, marked in the list. */
  current?: SubstrateRecord
  /** What the resolver says of the input, drawn under the heading. */
  status?: string
}) {
  const queryClient = useQueryClient()
  const [picking, setPicking] = useState<string | null>(null)
  const { authority, pkg, name: kindName } = splitKind(kind?.identity ?? "")
  const candidates = useInfiniteQuery({
    ...recordsInfiniteOptions({
      authority,
      package: pkg,
      name: kindName,
      first: PICKER_PAGE,
    }),
    enabled: Boolean(kind),
  })
  const options = candidates.data?.pages.flatMap((p) => p.records ?? []) ?? []

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

  const noun = kind?.name ?? "record"
  return (
    <Empty className="py-10">
      <EmptyHeader>
        <EmptyMedia variant="icon">
          <CircleDotIcon />
        </EmptyMedia>
        <EmptyTitle>
          Which {noun} is `{name}`?
        </EmptyTitle>
        <EmptyDescription>
          {input.description ?? `This app reads one ${noun} as ${name}.`}
          {status ? ` ${status}.` : ""}
        </EmptyDescription>
      </EmptyHeader>
      <EmptyContent>
        {candidates.isError && (
          <p className="text-sm text-destructive">
            The {noun} records could not be read: {candidates.error.message}
          </p>
        )}
        {candidates.isPending && kind && <Spinner className="size-4" />}
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
        {candidates.hasNextPage && (
          <Button
            variant="ghost"
            size="sm"
            disabled={candidates.isFetchingNextPage}
            onClick={() => void candidates.fetchNextPage()}
          >
            {candidates.isFetchingNextPage ? <Spinner /> : null}
            Load more
          </Button>
        )}
      </EmptyContent>
    </Empty>
  )
}
