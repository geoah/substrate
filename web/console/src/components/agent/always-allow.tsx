/** "Always allow this" on a suggested change a gate held, and the list of the
 * rules it wrote, each with Revoke. The rule is narrow by construction (one
 * agent, one kind, the verbs this request stands for) and lifts only the
 * gate that held the write (`lib/agent-rules.ts`, decision 0108). */

import { useState } from "react"
import { useMutation, useQueryClient } from "@tanstack/react-query"
import { ShieldCheckIcon } from "lucide-react"

import { KindRef } from "@/components/identity/kind-ref"
import { IdText } from "@/components/identity/id-text"
import { Button } from "@/components/ui/button"
import { ConfirmDialog } from "@/components/ui/confirm-dialog"
import { useTechnicalDetails } from "@/hooks/use-console-preferences"
import { agentName } from "@/lib/agent-chat"
import {
  allowConsequence,
  allowRuleRecords,
  allowVerbs,
  allowWords,
  type AllowRule,
  type StandingAllow,
} from "@/lib/agent-rules"
import { submitDecision } from "@/lib/api/changerequests"
import { CORE_AUTHORITY, CORE_PACKAGE_NAME } from "@/lib/api/http"
import { putRecord } from "@/lib/api/records"
import { deleteRecord } from "@/lib/api/sync"
import type { SubstrateRecord } from "@/lib/api/types"

const POLICY = "recordpatchpolicy"

/** Saves the rule, then applies this suggestion, behind one confirmation.
 * The rule lands first: a refused rule leaves the suggestion pending, never
 * applied under a rule that is not there. */
export function AlwaysAllowButton({
  request,
  rule,
  deleting,
}: {
  request: SubstrateRecord
  rule: AllowRule
  /** The suggestion deletes its record, which the confirmation says. */
  deleting: boolean
}) {
  const client = useQueryClient()
  const [technical] = useTechnicalDetails()
  const [open, setOpen] = useState(false)
  const [saved, setSaved] = useState(false)
  const save = useMutation({
    mutationFn: async () => {
      if (saved) {
        await submitDecision(request.id, "accepted", request.version)
        return
      }
      for (const { id, properties } of allowRuleRecords(rule)) {
        await putRecord(CORE_AUTHORITY, CORE_PACKAGE_NAME, POLICY, id, {
          properties,
          annotations: {
            "owner/provenance": `always allow, from change request ${request.id}`,
          },
        })
      }
      setSaved(true)
      await submitDecision(request.id, "accepted", request.version)
    },
    onSuccess: () => {
      setOpen(false)
      void client.invalidateQueries()
      // The resumed agent's reply lands a few seconds after the decision.
      setTimeout(() => void client.invalidateQueries(), 4000)
    },
    onError: () => void client.invalidateQueries(),
  })
  const name = agentName(rule.agent)
  return (
    <>
      <Button
        size="sm"
        variant="ghost"
        className="text-muted-foreground"
        onClick={() => {
          save.reset()
          setOpen(true)
        }}
      >
        Always allow this
      </Button>
      {open && (
        <ConfirmDialog
          title={`Always let ${name} ${allowWords(rule.kind, rule.ops)}?`}
          consequence={allowConsequence(name, rule)}
          confirm={saved ? "Try applying again" : "Apply and always allow"}
          destructive={deleting}
          pending={save.isPending}
          error={
            save.error
              ? saved
                ? `The rule is saved, but this suggestion wasn’t applied: ${save.error.message}`
                : `That didn’t go through: ${save.error.message}`
              : undefined
          }
          onConfirm={() => save.mutate()}
          onClose={() => setOpen(false)}
        >
          <p className="text-[13px] text-muted-foreground">
            {deleting
              ? "This suggestion is applied too, which deletes its record."
              : "This suggestion is applied too."}
          </p>
          {technical && (
            <pre className="overflow-x-auto rounded-md border bg-panel p-2 font-mono text-[11.5px]">
              {JSON.stringify(
                allowRuleRecords(rule).map((r) => r.properties),
                null,
                2
              )}
            </pre>
          )}
        </ConfirmDialog>
      )}
    </>
  )
}

/** The rules "Always allow this" wrote for one agent, each with Revoke. */
export function AllowRules({
  agent,
  rules,
}: {
  agent: string
  rules: StandingAllow[]
}) {
  const [technical] = useTechnicalDetails()
  const [revoking, setRevoking] = useState<StandingAllow>()
  const client = useQueryClient()
  const revoke = useMutation({
    mutationFn: async (rule: StandingAllow) => {
      for (const record of rule.records) await deleteRecord(record)
    },
    onSuccess: () => {
      setRevoking(undefined)
      void client.invalidateQueries()
    },
  })
  const name = agentName(agent)
  if (rules.length === 0) {
    return (
      <p className="text-[13px] text-muted-foreground">
        None yet. Choose Always allow this on a change it suggests, and it stops
        asking for that kind of change.
      </p>
    )
  }
  return (
    <>
      <ul className="flex flex-col gap-2" aria-label="Always allowed">
        {rules.map((rule) => (
          <li
            key={`${rule.kind}|${rule.gate}`}
            className="flex items-start gap-2 text-[13px]"
          >
            <ShieldCheckIcon
              aria-hidden
              className="mt-[3px] size-3.5 shrink-0 text-ok"
            />
            <span className="flex min-w-0 flex-1 flex-col gap-0.5">
              <span className="flex flex-wrap items-center gap-x-1.5 gap-y-0.5">
                <span className="first-letter:uppercase">
                  {allowVerbs(rule.ops)}
                </span>
                <KindRef kind={rule.kind} />
                <span>without asking</span>
              </span>
              {technical &&
                rule.records.map((r) => <IdText key={r.id} value={r.id} />)}
            </span>
            <Button
              size="xs"
              variant="ghost"
              className="text-muted-foreground"
              onClick={() => {
                revoke.reset()
                setRevoking(rule)
              }}
            >
              Revoke
            </Button>
          </li>
        ))}
      </ul>
      {revoking && (
        <ConfirmDialog
          title={`Stop letting ${name} ${allowWords(revoking.kind, revoking.ops)} without asking?`}
          consequence={`${name} will ask you again first, each time. What it already changed stays.`}
          confirm="Revoke"
          pending={revoke.isPending}
          error={
            revoke.error
              ? `That didn’t go through: ${revoke.error.message}`
              : undefined
          }
          onConfirm={() => revoke.mutate(revoking)}
          onClose={() => setRevoking(undefined)}
        />
      )}
    </>
  )
}
