/** Why a provider needs attention, under its page's header: one row per
 * problem `providerStanding` found, each saying what is wrong, the server's
 * own words for it, and the buttons that fix it. The card's line says the
 * first of them; this says all of them. */

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { RotateCcwIcon, TriangleAlertIcon } from "lucide-react"

import { ConnectButton } from "@/components/providers/account-actions"
import { TakeButton } from "@/components/providers/bundle-actions"
import {
  bundleTriggers,
  type ProviderEntry,
} from "@/components/providers/use-providers"
import { SyncNowButton } from "@/components/sync/sync-panel"
import { Button } from "@/components/ui/button"
import { Spinner } from "@/components/ui/spinner"
import { toast } from "@/components/ui/toast"
import {
  retryParked,
  triggerParkedQueryOptions,
  triggerRecordsQueryOptions,
  triggerStatusesQueryOptions,
} from "@/lib/api/sync"
import type { RequirementNode } from "@/lib/bundles"
import { scrollMotion } from "@/lib/motion"
import type { ProblemFix, ProviderProblem } from "@/lib/providers"
import { requestTriggers, triggersOnKind, type AccountView } from "@/lib/sync"
import { cn } from "@/lib/utils"

export function ProviderProblems({
  entry,
  chain,
  className,
}: {
  entry: ProviderEntry
  chain: RequirementNode[]
  className?: string
}) {
  const { problems } = entry.standing
  if (!problems.length) return null
  return (
    <section
      aria-label="Why it needs attention"
      data-slot="provider-problems"
      className={cn(
        "flex items-start gap-2.5 rounded-lg bg-bad-soft px-3.5 py-3 text-[13px]",
        className
      )}
    >
      <TriangleAlertIcon
        aria-hidden
        className="mt-0.5 size-4 shrink-0 text-destructive"
      />
      <div className="min-w-0 flex-1">
        <p className="font-medium">
          {problems.length === 1
            ? "Why it needs attention"
            : `${problems.length} things need your attention`}
        </p>
        <ul className="mt-1.5 flex flex-col divide-y divide-border/60">
          {problems.map((p, i) => (
            <ProblemRow key={i} problem={p} entry={entry} chain={chain} />
          ))}
        </ul>
      </div>
    </section>
  )
}

/** Guard lines and trigger errors are the server's own references; an
 * account's error is a sentence. */
const MONO_DETAIL: ProviderProblem["code"][] = [
  "update-blocked",
  "trigger-broken",
  "failed-to-load",
]

function ProblemRow({
  problem,
  entry,
  chain,
}: {
  problem: ProviderProblem
  entry: ProviderEntry
  chain: RequirementNode[]
}) {
  const account = entry.view?.accounts.find(
    (a) => a.record.id === problem.account
  )
  return (
    <li
      data-problem={problem.code}
      className="flex flex-wrap items-start gap-x-4 gap-y-2 py-2 first:pt-0.5 last:pb-0"
    >
      <div className="min-w-0 flex-1 basis-64 space-y-0.5">
        <p>{problem.summary}</p>
        {problem.detail?.map((line) => (
          <p
            key={line}
            className={cn(
              "break-words text-muted-foreground",
              MONO_DETAIL.includes(problem.code) && "font-mono text-xs"
            )}
          >
            {line}
          </p>
        ))}
      </div>
      {problem.fixes.length > 0 && (
        <div className="flex shrink-0 flex-wrap items-center gap-1.5">
          {problem.fixes.map((fix) => (
            <FixButton
              key={fix}
              fix={fix}
              entry={entry}
              chain={chain}
              account={account}
              setupTarget={
                problem.code === "credentials-missing" ? "setup" : "settings"
              }
            />
          ))}
        </div>
      )}
    </li>
  )
}

function FixButton({
  fix,
  entry,
  chain,
  account,
  setupTarget,
}: {
  fix: ProblemFix
  entry: ProviderEntry
  chain: RequirementNode[]
  account?: AccountView
  setupTarget: "setup" | "settings"
}) {
  const { info, row, view } = entry
  switch (fix) {
    case "sync-now":
      return account ? <AccountSyncNow account={account} /> : null
    case "reconnect":
      return account && view?.oauth ? (
        <ConnectButton
          view={account}
          providerName={info.name}
          reconnect
          disabled={!view.configured}
        />
      ) : null
    case "add-again":
      return row.catalog ? (
        <TakeButton
          row={row}
          chain={chain}
          name={info.name}
          label="Add again"
        />
      ) : null
    case "set-up":
      return (
        <Button
          variant="outline"
          size="sm"
          onClick={() =>
            document
              .getElementById(setupTarget)
              ?.scrollIntoView?.({ behavior: scrollMotion(), block: "start" })
          }
        >
          {setupTarget === "settings" ? "Go to settings" : "Go to set up"}
        </Button>
      )
    case "retry-parked":
      return <RetryParkedButton bundleId={row.id} />
  }
}

function AccountSyncNow({ account }: { account: AccountView }) {
  const triggers = useQuery(triggerRecordsQueryOptions)
  const ids = requestTriggers(
    triggersOnKind(triggers.data ?? [], account.record.kind)
  ).map((s) => s.id)
  return (
    <SyncNowButton
      record={account.record}
      paused={account.sync.paused}
      requestTriggerIds={ids}
      className="h-8 px-2.5 text-[13px]"
    />
  )
}

/** Retries every parked run of the provider's triggers, one at a time, and
 * says how many went through. */
function RetryParkedButton({ bundleId }: { bundleId: string }) {
  const queryClient = useQueryClient()
  const statuses = useQuery(triggerStatusesQueryOptions)
  const retry = useMutation({
    mutationFn: async () => {
      let retried = 0
      const parked = bundleTriggers(statuses.data ?? [], bundleId).filter(
        (t) => t.parked > 0
      )
      for (const t of parked) {
        const failures = await queryClient.fetchQuery({
          ...triggerParkedQueryOptions(t.id),
          staleTime: 0,
        })
        for (const f of failures) {
          await retryParked(t.id, f.id)
          retried++
        }
      }
      return retried
    },
    onSuccess: (retried) =>
      toast.add({
        type: "success",
        title: retried === 1 ? "Retried 1 run" : `Retried ${retried} runs`,
        description:
          "Each one ran again; any that fails again is parked again.",
      }),
    onError: (error) =>
      toast.add({
        type: "error",
        title: "Retry failed",
        description: error.message,
      }),
    onSettled: () =>
      void queryClient.invalidateQueries({ queryKey: ["triggers"] }),
  })
  return (
    <Button
      variant="outline"
      size="sm"
      disabled={retry.isPending || !statuses.data}
      onClick={() => retry.mutate()}
    >
      {retry.isPending ? (
        <Spinner className="size-3" />
      ) : (
        <RotateCcwIcon className="size-3.5" />
      )}
      Retry
    </Button>
  )
}
