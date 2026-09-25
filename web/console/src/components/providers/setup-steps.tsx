/** A provider's set-up as four numbered steps: add it, give it sign-in
 * details, connect an account, choose what to bring in. A done step wears a
 * green check and says what is true; the current step is highlighted with
 * its one primary action; the steps after it are muted and say what comes
 * next. The dialogs each step opens are the existing ones: the sign-in
 * details form and the account form, whose create-and-connect press opens
 * the provider's consent. */

import { useState, type ReactNode } from "react"
import { CheckIcon } from "lucide-react"

import { OAuthCallbackNote } from "@/components/oauth-callback-note"
import { ConnectButton } from "@/components/providers/account-actions"
import { TakeButton } from "@/components/providers/bundle-actions"
import type { ProviderEntry } from "@/components/providers/use-providers"
import { AccountDialog } from "@/components/sync/account-dialog"
import { CredentialsDialog } from "@/components/sync/credentials-dialog"
import { Button } from "@/components/ui/button"
import { useOAuthConnect } from "@/hooks/use-oauth-connect"
import type { RequirementNode } from "@/lib/bundles"
import {
  andList,
  choiceSentence,
  type SetupStep,
  type StepKey,
} from "@/lib/providers"
import type { AccountView } from "@/lib/sync"
import { cn } from "@/lib/utils"

const TITLES: Record<StepKey, (name: string) => string> = {
  add: (name) => `Add ${name}`,
  credentials: () => "Sign-in details",
  account: () => "Connect your account",
  choose: () => "Choose what to bring in",
}

export function SetupSteps({
  entry,
  chain,
}: {
  entry: ProviderEntry
  chain: RequirementNode[]
}) {
  const { info, view, row, toggles } = entry
  const name = info.name
  const [credentials, setCredentials] = useState(false)
  const [adding, setAdding] = useState(false)
  const [choosing, setChoosing] = useState<AccountView | null>(null)
  // Owned here rather than by the dialog, so the consent's return is still
  // heard after the dialog that started it has closed.
  const connect = useOAuthConnect()
  const accounts = view?.accounts ?? []
  const waiting = view?.oauth
    ? accounts.find((a) => a.tokenStatus !== "connected")
    : undefined
  const connected = accounts.filter(
    (a) => !view?.oauth || a.tokenStatus === "connected"
  )
  const first = connected[0] ?? accounts[0]

  function body(step: SetupStep): { text?: ReactNode; action?: ReactNode } {
    const now = step.state === "now"
    const done = step.state === "done"
    switch (step.key) {
      case "add":
        return now
          ? {
              text: `Adds ${name}’s collections and tools to your substrate. Nothing is copied in until you connect an account.`,
              action: (
                <TakeButton
                  row={row}
                  chain={chain}
                  name={name}
                  label={
                    row.status?.quarantined ? "Add again" : `Add ${name}`
                  }
                  variant="default"
                />
              ),
            }
          : {}
      case "credentials":
        if (!view?.configKind) {
          return done
            ? { text: `${name} needs no sign-in details of its own.` }
            : { text: `Next, after adding ${name}.` }
        }
        if (done) {
          return {
            text: view.oauth
              ? `Your ${name} app’s client ID and secret are saved.`
              : `Your ${name} token is saved.`,
            action: (
              <Button
                variant="outline"
                size="sm"
                onClick={() => setCredentials(true)}
              >
                Change
              </Button>
            ),
          }
        }
        if (!now) return { text: `Next, after adding ${name}.` }
        return {
          text: (
            <>
              <span>
                {view.oauth
                  ? `${name} needs an app of yours to sign in with. Create one in ${name}’s developer settings and paste its client ID and secret here.`
                  : `${name} needs a token of yours to sign in with. Create one in ${name}’s settings and paste it here.`}
              </span>
              {view.oauth && (
                <OAuthCallbackNote providerId={row.id} className="mt-2" />
              )}
            </>
          ),
          action: (
            <Button size="sm" onClick={() => setCredentials(true)}>
              Add details
            </Button>
          ),
        }
      case "account":
        if (!view?.accountKind) {
          return done
            ? { text: "Nothing to connect: it works without an account." }
            : {}
        }
        if (done) {
          const labels = connected.map((a) => a.label)
          return {
            text: `${andList(labels.slice(0, 2))}${
              labels.length > 2 ? ` and ${labels.length - 2} more` : ""
            }, connected.`,
            action: (
              <Button variant="outline" size="sm" onClick={() => setAdding(true)}>
                Add another
              </Button>
            ),
          }
        }
        if (!now) return { text: "Next, after the sign-in details." }
        if (waiting) {
          return {
            text: `${waiting.label} is waiting for you to approve it at ${name}.`,
            action: (
              <ConnectButton
                view={waiting}
                providerName={name}
                variant="default"
              />
            ),
          }
        }
        return {
          text: view.oauth
            ? `Sign in to ${name} and approve access. You choose what to bring in first.`
            : `Add an account and choose what to bring in. It starts on its own.`,
          action: (
            <Button size="sm" onClick={() => setAdding(true)}>
              Connect an account
            </Button>
          ),
        }
      case "choose":
        if (!view?.accountKind) return {}
        if (done && first) {
          return {
            text:
              accounts.length > 1
                ? `${first.label}: ${choiceSentence(first.record, toggles)}`
                : choiceSentence(first.record, toggles),
            action: toggles.length ? (
              <Button
                variant="outline"
                size="sm"
                onClick={() => setChoosing(first)}
              >
                Change
              </Button>
            ) : undefined,
          }
        }
        if (now && first) {
          return {
            text: `Turn on what ${first.label} should bring in.`,
            action: (
              <Button size="sm" onClick={() => setChoosing(first)}>
                Choose
              </Button>
            ),
          }
        }
        return {
          text: toggles.length
            ? `${andList(toggles.map((t) => t.label))}, each with its own switch.`
            : "Everything it can bring in.",
        }
    }
  }

  return (
    <>
      <ol
        data-slot="setup-steps"
        className="overflow-hidden rounded-[10px] border"
      >
        {entry.steps.map((step) => {
          const { text, action } = body(step)
          return (
            <li
              key={step.key}
              data-state={step.state}
              aria-current={step.state === "now" ? "step" : undefined}
              className={cn(
                "grid grid-cols-[26px_minmax(0,1fr)_auto] items-start gap-3 border-b px-4 py-3.5 last:border-b-0",
                step.state === "now" && "bg-primary-soft/50"
              )}
            >
              <span
                aria-hidden
                className={cn(
                  "grid size-[22px] place-items-center rounded-full border-[1.5px] text-[11.5px] font-semibold",
                  step.state === "done" && "border-ok bg-ok text-white",
                  step.state === "now" && "border-primary text-primary-text",
                  step.state === "todo" && "border-border-strong text-faint"
                )}
              >
                {step.state === "done" ? (
                  <CheckIcon className="size-3" strokeWidth={3} />
                ) : (
                  step.n
                )}
              </span>
              <div className="min-w-0">
                <div
                  className={cn(
                    "font-medium",
                    step.state === "done" && "text-muted-foreground",
                    step.state === "todo" && "text-muted-foreground"
                  )}
                >
                  <span className="sr-only">
                    Step {step.n}
                    {step.state === "done"
                      ? ", done: "
                      : step.state === "now"
                        ? ", current: "
                        : ": "}
                  </span>
                  {TITLES[step.key](name)}
                </div>
                {text && (
                  <div
                    className={cn(
                      "mt-0.5 max-w-[60ch] text-[12.5px]",
                      step.state === "todo" ? "text-faint" : "text-muted-foreground"
                    )}
                  >
                    {text}
                  </div>
                )}
              </div>
              <div className="flex items-center gap-2">{action}</div>
            </li>
          )
        })}
      </ol>
      {credentials && view && (
        <CredentialsDialog
          provider={view}
          providerName={name}
          open={credentials}
          onOpenChange={setCredentials}
        />
      )}
      {adding && view?.accountKind && (
        <AccountDialog
          kind={view.accountKind}
          providerName={name}
          oauth={view.oauth}
          configured={
            view.configured &&
            Boolean(view.status?.installed && view.status.enabled)
          }
          connect={view.oauth ? connect : undefined}
          onSetUpCredentials={
            view.configKind
              ? () => {
                  setAdding(false)
                  setCredentials(true)
                }
              : undefined
          }
          open={adding}
          onOpenChange={setAdding}
        />
      )}
      {choosing && view?.accountKind && (
        <AccountDialog
          kind={choosing.kind ?? view.accountKind}
          providerName={name}
          oauth={view.oauth}
          configured={view.configured}
          record={choosing.record}
          label={choosing.label}
          open
          onOpenChange={(open) => !open && setChoosing(null)}
        />
      )}
    </>
  )
}
