/** The repository's daily spend cap (`substrate.reamde.dev/llm/spendCentsPerDay`,
 * a core `setting` record nothing seeds): the cap in dollars and what every
 * agent's runs spent in the last 24 hours against it, summed from one read of
 * the newest runs the way the agent page sums one agent's. Editing writes the
 * record in cents; an empty cap writes an empty value, which is no cap. */

import { useState, type ReactNode } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"

import { EmptyValue } from "@/components/identity/empty-value"
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
  repositorySpendCap,
  type CapSpend,
} from "@/lib/agent-runs"
import { repositoryRunsQueryOptions } from "@/lib/api/agents"
import {
  repositorySpendCapQueryOptions,
  saveRepositorySpendCap,
} from "@/lib/api/settings"

const CAP_LABEL = "Cap"
const SPENT_LABEL = "Spent in the last 24 hours"
const CAP_HINT = "In US dollars, to the cent. Empty is no cap."

export function SpendCapRow() {
  const setting = useQuery(repositorySpendCapQueryOptions)
  const runs = useQuery(repositoryRunsQueryOptions())
  const [editing, setEditing] = useState(false)

  const cap = repositorySpendCap(setting.data)
  const spend = runs.isSuccess
    ? capWindowSpend(runs.data.records ?? [], {
        truncated: Boolean(runs.data.cursor),
      })
    : undefined
  const reached =
    cap !== undefined && spend !== undefined && spend.usd * 100 >= cap

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
            {cap === undefined ? "Set a cap" : "Change"}
          </Button>
        )
      }
    >
      <Facts
        rows={[
          [
            CAP_LABEL,
            editing ? (
              <CapEditor cap={cap} onDone={() => setEditing(false)} />
            ) : setting.isPending ? (
              <span className="text-faint">Reading…</span>
            ) : setting.isError ? (
              <EmptyValue>
                The cap didn’t load: {setting.error.message}
              </EmptyValue>
            ) : cap === undefined ? (
              <EmptyValue>No cap</EmptyValue>
            ) : (
              <span className="tabular-nums">
                {capWords(cap)} in any 24 hours
              </span>
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
          agents it asked.
        </p>
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
  onDone,
}: {
  cap: number | undefined
  onDone: () => void
}) {
  const queryClient = useQueryClient()
  const [text, setText] = useState(capInputText(cap))
  const parsed = parseCapDollars(text)
  const error = "error" in parsed ? parsed.error : undefined

  const save = useMutation({
    mutationFn: saveRepositorySpendCap,
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
            placeholder="No cap"
            onChange={(e) => setText(e.target.value)}
          />
        </InputGroup>
        <Button
          type="submit"
          size="sm"
          disabled={error !== undefined || save.isPending}
        >
          {save.isPending && <Spinner />}
          Save
        </Button>
        {cap !== undefined && (
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
      <p
        id="spend-cap-hint"
        className={
          error ? "text-[12.5px] text-destructive" : "text-[12.5px] text-faint"
        }
      >
        {error ?? CAP_HINT}
      </p>
    </form>
  )
}

function Facts({ rows }: { rows: Array<[string, ReactNode]> }) {
  return (
    <dl className="grid grid-cols-[minmax(96px,120px)_minmax(0,1fr)] gap-x-3 border-t sm:grid-cols-[minmax(120px,190px)_minmax(0,1fr)]">
      {rows.map(([label, value]) => (
        <div key={label} className="contents">
          <dt className="py-2.5 text-[13.5px] text-muted-foreground">
            {label}
          </dt>
          <dd className="min-w-0 py-2.5 text-[13.5px]">{value}</dd>
        </div>
      ))}
    </dl>
  )
}
