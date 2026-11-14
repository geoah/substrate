/** One agent's daily spend cap on its page (decision record 0314): the cap
 * its runs are held to, who set it, and what counts against it, with the
 * owner's own cap set, changed and removed here. The owner's cap lives on
 * the agent's `llm/agentbudget`, one per agent, and replaces the cap the
 * agent declares whoever's key pays. */

import { useState } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"

import { EmptyValue } from "@/components/identity/empty-value"
import { Facts } from "@/components/identity/facts"
import { Pill } from "@/components/identity/pill"
import { SectionHead } from "@/components/identity/section-head"
import { Button } from "@/components/ui/button"
import {
  InputGroup,
  InputGroupAddon,
  InputGroupInput,
} from "@/components/ui/input-group"
import { Spinner } from "@/components/ui/spinner"
import { toast } from "@/components/ui/toast"
import {
  agentCapInForce,
  capInputText,
  capWords,
  costWords,
  parseCapDollars,
  reachedCap,
  type AgentCapInForce,
  type CapSpend,
} from "@/lib/agent-runs"
import {
  agentBudgetQueryOptions,
  removeAgentBudget,
  saveAgentSpendCap,
} from "@/lib/api/agents"
import type { SubstrateRecord } from "@/lib/api/types"

const CAP_LABEL = "Cap"
const SPENT_LABEL = "Spent in the last 24 hours"
const CAP_HINT = "In US dollars, to the cent."

export function AgentSpendCap({
  agentId,
  name,
  declared,
  spend,
}: {
  agentId: string
  /** The agent's name, as the page titles it. */
  name: string
  /** The agent's `budgets.spendCentsPerDay`, where it declares one. */
  declared: number | undefined
  /** What counts against the cap, once the agent's runs are read. */
  spend: CapSpend | undefined
}) {
  const budget = useQuery(agentBudgetQueryOptions(agentId))
  const [editing, setEditing] = useState(false)
  const cap = agentCapInForce(declared, budget.data)
  const reached =
    cap.source !== "none" &&
    spend !== undefined &&
    reachedCap(spend.usd, cap.cents)

  return (
    <>
      <SectionHead
        title="Daily spend cap"
        hint="what its runs may spend in any 24 hours"
        actions={
          !editing && (
            <Button
              variant="outline"
              size="sm"
              disabled={!budget.isSuccess}
              onClick={() => setEditing(true)}
            >
              {cap.source === "own" ? "Change" : "Set a cap"}
            </Button>
          )
        }
      />
      <Facts
        rows={[
          [
            CAP_LABEL,
            editing ? (
              <CapEditor
                agentId={agentId}
                name={name}
                budget={budget.data ?? null}
                cap={cap}
                declared={declared}
                onDone={() => setEditing(false)}
              />
            ) : budget.isPending ? (
              <span className="text-faint">Reading…</span>
            ) : budget.isError ? (
              <EmptyValue>
                The cap didn’t load: {budget.error.message}
              </EmptyValue>
            ) : cap.source === "none" ? (
              <EmptyValue>No cap</EmptyValue>
            ) : (
              <span className="tabular-nums">
                {capWords(cap.cents)} in any 24 hours
              </span>
            ),
          ],
          [
            SPENT_LABEL,
            spend === undefined ? (
              <EmptyValue>Its runs didn’t load</EmptyValue>
            ) : (
              <span className="flex flex-wrap items-center gap-2">
                <span className="tabular-nums">
                  {!spend.complete && (
                    <span className="text-faint">at least </span>
                  )}
                  {costWords(spend.usd)}
                </span>
                {reached && <Pill tone="warn">At the cap</Pill>}
              </span>
            ),
          ],
        ]}
      />
      <div className="mt-2 flex flex-col gap-0.5 text-[12.5px] text-faint">
        {budget.isSuccess && <p>{capSource(cap, declared)}</p>}
        {reached && (
          <p>
            Its triggers hold their deliveries, and a chat with it is refused,
            until its spend falls under the cap or you raise it. A held delivery
            waits and runs then.
          </p>
        )}
        <p>
          Counts the cost each run it started recorded when it finished,
          including the agents it asked.
        </p>
        {spend !== undefined && spend.running > 0 && (
          <p>
            A run still going counts against the cap as it spends, and shows
            here once it finishes.
          </p>
        )}
      </div>
    </>
  )
}

/** Who set the cap that holds, and what it takes the place of. */
function capSource(cap: AgentCapInForce, declared: number | undefined) {
  switch (cap.source) {
    case "own":
      return declared === undefined
        ? "Set by you."
        : `Set by you, in place of the ${capWords(declared)} it declares.`
    case "declared":
      return "Declared by the agent. A cap you set replaces it."
    case "none":
      return "It declares none. A cap you set holds its runs."
  }
}

function CapEditor({
  agentId,
  name,
  budget,
  cap,
  declared,
  onDone,
}: {
  agentId: string
  name: string
  budget: SubstrateRecord | null
  cap: AgentCapInForce
  declared: number | undefined
  onDone: () => void
}) {
  const queryClient = useQueryClient()
  const [text, setText] = useState(
    cap.source === "none"
      ? ""
      : capInputText({ state: "set", cents: cap.cents })
  )
  const parsed = parseCapDollars(text)
  // A blank field is not an error yet, only not a cap: Save waits for one.
  const blank = text.trim() === ""
  const error = "error" in parsed && !blank ? parsed.error : undefined
  const fallback =
    declared === undefined
      ? "it has no cap"
      : `the ${capWords(declared)} it declares holds again`
  // A write that failed may have landed all the same, so the cached
  // override is read again either way.
  const reread = () =>
    queryClient.invalidateQueries({
      queryKey: agentBudgetQueryOptions(agentId).queryKey,
    })

  const save = useMutation({
    mutationFn: (cents: number | null) =>
      cents !== null
        ? saveAgentSpendCap(agentId, cents, budget)
        : budget
          ? removeAgentBudget(budget)
          : Promise.resolve(),
    onSuccess: (_, cents) => {
      toast.add({
        type: "success",
        title:
          cents === null
            ? `Your cap for ${name} is removed: ${fallback}.`
            : `${name}’s daily spend cap is ${capWords(cents)}.`,
      })
      void reread()
      onDone()
    },
    onError: (err) => {
      toast.add({
        type: "error",
        title: "Saving the cap failed",
        description: err.message,
      })
      void reread()
    },
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
            aria-describedby="agent-spend-cap-hint"
            inputMode="decimal"
            autoFocus
            value={text}
            placeholder="10.00"
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
        {budget && (
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={save.isPending}
            onClick={() => save.mutate(null)}
          >
            Remove your cap
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
        <p id="agent-spend-cap-hint" className="text-[12.5px] text-faint">
          {CAP_HINT}
          {budget && ` Removing yours: ${fallback}.`}
        </p>
      ) : (
        <p
          id="agent-spend-cap-hint"
          role="alert"
          className="text-[12.5px] text-destructive"
        >
          {error}
        </p>
      )}
    </form>
  )
}
