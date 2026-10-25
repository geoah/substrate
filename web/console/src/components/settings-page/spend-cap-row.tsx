/** The repository's daily spend cap (`substrate.reamde.dev/llm/spendCentsPerDay`,
 * a core `setting` record nothing seeds): the cap in dollars and what every
 * agent's runs spent in the last 24 hours against it, summed from one read of
 * the newest runs, live and deleted, the way the agent page sums one agent's.
 * Editing writes the record in cents; removing the cap writes an empty value,
 * which is no cap. */

import { useState } from "react"
import {
  useMutation,
  useQuery,
  useQueryClient,
  type UseQueryResult,
} from "@tanstack/react-query"

import { EmptyValue } from "@/components/identity/empty-value"
import { Facts } from "@/components/identity/facts"
import { Pill } from "@/components/identity/pill"
import { SettingRow } from "@/components/settings-page/setting-row"
import { Button } from "@/components/ui/button"
import {
  InputGroup,
  InputGroupAddon,
  InputGroupInput,
} from "@/components/ui/input-group"
import { Spinner } from "@/components/ui/spinner"
import { toast } from "@/components/ui/toast"
import {
  capInputText,
  capWindowSpend,
  capWords,
  costWords,
  parseCapDollars,
  reachedCap,
  repositorySpendCap,
  type CapSpend,
  type RepositorySpendCap,
} from "@/lib/agent-runs"
import { repositoryRunsQueryOptions } from "@/lib/api/agents"
import {
  repositorySpendCapQueryOptions,
  saveRepositorySpendCap,
} from "@/lib/api/settings"
import type { Page, SubstrateRecord } from "@/lib/api/types"

const CAP_LABEL = "Cap"
const SPENT_LABEL = "Spent in the last 24 hours"
const CAP_HINT = "In US dollars, to the cent."

export function SpendCapRow() {
  const setting = useQuery(repositorySpendCapQueryOptions)
  const runs = useQuery(repositoryRunsQueryOptions())
  const deletedRuns = useQuery(repositoryRunsQueryOptions({ deleted: true }))
  const [editing, setEditing] = useState(false)

  const cap = repositorySpendCap(setting.data)
  const spend = repositorySpend(runs, deletedRuns)
  const reached =
    cap.state === "set" &&
    spend !== undefined &&
    reachedCap(spend.usd, cap.cents)

  return (
    <SettingRow
      title="Daily spend cap"
      description="What every agent’s runs together may spend in any 24 hours. An agent’s own cap applies beside it."
      control={
        !editing && (
          <Button
            variant="outline"
            size="sm"
            disabled={!setting.isSuccess}
            onClick={() => setEditing(true)}
          >
            {cap.state === "none" ? "Set a cap" : "Change"}
          </Button>
        )
      }
    >
      <Facts
        rows={[
          [
            CAP_LABEL,
            editing ? (
              <CapEditor
                cap={cap}
                exists={Boolean(setting.data)}
                onDone={() => setEditing(false)}
              />
            ) : setting.isPending ? (
              <span className="text-faint">Reading…</span>
            ) : setting.isError ? (
              <EmptyValue>
                The cap didn’t load: {setting.error.message}
              </EmptyValue>
            ) : (
              <CapValue cap={cap} />
            ),
          ],
          [
            SPENT_LABEL,
            <Spent spend={spend} failed={runs.isError} reached={reached} />,
          ],
        ]}
      />
      <div className="mt-2 flex flex-col gap-0.5 text-[12.5px] text-faint">
        {reached && (
          <p>
            Every agent’s triggers hold their deliveries, and chats are refused,
            until the spend falls under the cap or you raise it. A held delivery
            waits and runs then.
          </p>
        )}
        <p>
          Counts the cost each run recorded when it finished, including the
          agents it asked and runs deleted since.
        </p>
        {spend !== undefined && !spend.complete && (
          <p>
            {deletedRuns.isError
              ? "The deleted runs didn’t load, so this counts only the runs still here."
              : "There are more runs than one read holds, so some that finished in the window aren’t counted here."}
          </p>
        )}
        {spend !== undefined && spend.running > 0 && (
          <p>
            A run still going counts against the cap as it spends, and shows
            here once it finishes.
          </p>
        )}
      </div>
    </SettingRow>
  )
}

/** The window's spend over the live runs and the deleted ones together.
 * Without the deleted runs the sum is only a floor, so a failed read of them
 * marks it incomplete rather than leaving it to look whole. The two reads are
 * separate snapshots, so a run deleted between them is in both: it counts
 * once, as the newer of the two. */
function repositorySpend(
  runs: UseQueryResult<Page>,
  deletedRuns: UseQueryResult<Page>
): CapSpend | undefined {
  if (!runs.isSuccess) return undefined
  const liveList = runs.data.records ?? []
  const live = capWindowSpend(liveList, {
    truncated: Boolean(runs.data.cursor),
  })
  if (deletedRuns.isError) return { ...live, complete: false }
  if (!deletedRuns.isSuccess) return undefined
  const deletedList = deletedRuns.data.records ?? []
  const deleted = capWindowSpend(deletedList, {
    truncated: Boolean(deletedRuns.data.cursor),
  })
  const newest = newestById([...liveList, ...deletedList])
  const kept = (r: SubstrateRecord) => newest.get(recordKey(r)) === r
  const keptLive = capWindowSpend(liveList.filter(kept))
  return {
    usd: keptLive.usd + capWindowSpend(deletedList.filter(kept)).usd,
    complete: live.complete && deleted.complete,
    running: keptLive.running,
  }
}

const recordKey = (r: SubstrateRecord) => `${r.kind}/${r.id}`

/** Each record by kind and id, the most recently updated of its copies; a
 * tie goes to the later one, so the deleted read's tombstone wins. */
function newestById(records: SubstrateRecord[]): Map<string, SubstrateRecord> {
  const newest = new Map<string, SubstrateRecord>()
  for (const r of records) {
    const held = newest.get(recordKey(r))
    if (!held || !(Date.parse(r.updatedAt) < Date.parse(held.updatedAt))) {
      newest.set(recordKey(r), r)
    }
  }
  return newest
}

function CapValue({ cap }: { cap: RepositorySpendCap }) {
  switch (cap.state) {
    case "none":
      return <EmptyValue>No cap</EmptyValue>
    case "set":
      return (
        <span className="tabular-nums">
          {capWords(cap.cents)} in any 24 hours
        </span>
      )
    case "unreadable":
      return (
        <span className="flex flex-col gap-1">
          <span className="flex flex-wrap items-center gap-2">
            <Pill tone="warn">Unreadable</Pill>
            <code className="font-mono text-[12.5px] break-all">
              {cap.value}
            </code>
          </span>
          <span className="text-[12.5px] text-muted-foreground">
            Every agent run is held until this is a whole number of cents.
          </span>
        </span>
      )
  }
}

function Spent({
  spend,
  failed,
  reached,
}: {
  spend?: CapSpend
  failed: boolean
  reached: boolean
}) {
  if (spend === undefined) {
    return failed ? (
      <EmptyValue>The runs didn’t load</EmptyValue>
    ) : (
      <span className="text-faint">Reading…</span>
    )
  }
  return (
    <span className="flex flex-wrap items-center gap-2">
      <span className="tabular-nums">
        {!spend.complete && <span className="text-faint">at least </span>}
        {costWords(spend.usd)}
      </span>
      {reached && <Pill tone="warn">At the cap</Pill>}
    </span>
  )
}

function CapEditor({
  cap,
  exists,
  onDone,
}: {
  cap: RepositorySpendCap
  exists: boolean
  onDone: () => void
}) {
  const queryClient = useQueryClient()
  const [text, setText] = useState(capInputText(cap))
  const parsed = parseCapDollars(text)
  // A blank field is not an error yet, only not a cap: Save waits for one.
  const blank = text.trim() === ""
  const error = "error" in parsed && !blank ? parsed.error : undefined

  const save = useMutation({
    mutationFn: (cents: number | null) =>
      saveRepositorySpendCap({ cents, exists }),
    onSuccess: (_, cents) => {
      toast.add({
        type: "success",
        title:
          cents === null
            ? "The daily spend cap is off."
            : `The daily spend cap is ${capWords(cents)}.`,
      })
      void queryClient.invalidateQueries({
        queryKey: repositorySpendCapQueryOptions.queryKey,
      })
      onDone()
    },
    onError: (err) =>
      toast.add({
        type: "error",
        title: "Saving the cap failed",
        description: err.message,
      }),
  })

  return (
    <form
      aria-label="Daily spend cap"
      className="flex flex-col gap-1.5"
      onSubmit={(e) => {
        e.preventDefault()
        if ("cents" in parsed) save.mutate(parsed.cents)
      }}
    >
      <div className="flex flex-wrap items-center gap-2">
        <InputGroup className="w-40">
          <InputGroupAddon>$</InputGroupAddon>
          <InputGroupInput
            aria-label={CAP_LABEL}
            aria-invalid={error !== undefined}
            aria-describedby="spend-cap-hint"
            inputMode="decimal"
            autoFocus
            value={text}
            placeholder="20.00"
            onChange={(e) => setText(e.target.value)}
          />
        </InputGroup>
        <Button
          type="submit"
          size="sm"
          disabled={!("cents" in parsed) || save.isPending}
        >
          {save.isPending && <Spinner />}
          Save
        </Button>
        {cap.state !== "none" && (
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={save.isPending}
            onClick={() => save.mutate(null)}
          >
            Remove the cap
          </Button>
        )}
        <Button
          type="button"
          variant="ghost"
          size="sm"
          disabled={save.isPending}
          onClick={onDone}
        >
          Cancel
        </Button>
      </div>
      {error === undefined ? (
        <p id="spend-cap-hint" className="text-[12.5px] text-faint">
          {CAP_HINT}
        </p>
      ) : (
        <p
          id="spend-cap-hint"
          role="alert"
          className="text-[12.5px] text-destructive"
        >
          {error}
        </p>
      )}
    </form>
  )
}
