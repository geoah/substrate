/** The welcome at the top of Providers, shown where the deployment loaded an
 * operator defaults file (`operatorDefaults` in server.json) until the
 * reader dismisses it: what works now on the operator's keys, the accounts
 * that wait on the reader, and that any key can be their own. Registration
 * lands here on such a deployment. **Done** dismisses it for every browser,
 * because the fact lives on the console preference record. */

import type { ReactNode } from "react"
import { Link } from "@tanstack/react-router"

import { Pill } from "@/components/identity/pill"
import { pageTitleClass } from "@/components/identity/page-title"
import { Button, buttonVariants } from "@/components/ui/button"
import type { OperatorDefault } from "@/lib/api/types"
import {
  sentenceCase,
  welcomeFacts,
  type OperatorWords,
} from "@/lib/operator-defaults"
import { andList } from "@/lib/providers"
import { cn } from "@/lib/utils"

function Step({
  pill,
  title,
  children,
}: {
  pill: ReactNode
  title: string
  children: ReactNode
}) {
  return (
    <li className="rounded-lg border border-border bg-background px-3 py-2.5">
      <div className="flex flex-wrap items-center gap-2">
        {pill}
        <span className="font-medium">{title}</span>
      </div>
      <div className="mt-1 text-faint">{children}</div>
    </li>
  )
}

export function ProvidersWelcome({
  words,
  items,
  allowance,
  onDone,
}: {
  words: OperatorWords
  items: readonly OperatorDefault[]
  /** The operator pays up to a daily allowance. */
  allowance: boolean
  onDone: () => void
}) {
  const { works, connect } = welcomeFacts(items)
  const chat = works.includes("chat")
  const limit = allowance ? ", up to a daily allowance" : ""
  return (
    <section
      aria-labelledby="providers-welcome"
      className="rounded-[12px] border border-border bg-panel px-6 py-5"
    >
      <div className="text-[13px] font-medium text-primary-text">
        {words.named ? `Welcome to ${words.name}` : "Welcome"}
      </div>
      <h1 id="providers-welcome" className={cn(pageTitleClass("page"), "mt-1")}>
        {chat ? "Your agents can start working now" : "What you can use here"}
      </h1>
      <p className="mt-2 max-w-[64ch] text-muted-foreground">
        {works.length > 0
          ? `${words.Name} pays for ${andList(works)}${limit}, so you can use them today. `
          : ""}
        Your own accounts are yours to connect. Anywhere you add your own key,
        it is used instead of {words.possessive}.
      </p>
      <ol className="mt-4 grid gap-2 text-[13.5px] sm:grid-cols-3">
        {works.length > 0 && (
          <Step
            pill={<Pill tone="ok">On</Pill>}
            title={sentenceCase(andList(works))}
          >
            Paid by {words.name}. Nothing to set up.
          </Step>
        )}
        <Step
          pill={<Pill tone="warn">Set up</Pill>}
          title={
            connect.length > 0
              ? `Connect ${andList(connect)}`
              : "Connect your accounts"
          }
        >
          {connect.length > 0
            ? "One approval brings your data in."
            : "Bring in mail, calendars and more, under Accounts you connect."}
        </Step>
        <Step
          pill={
            <Pill tone="neutral" dot={false}>
              Optional
            </Pill>
          }
          title="Your own keys"
        >
          Replace any of {words.possessive} under AI models.
        </Step>
      </ol>
      <div className="mt-4 flex flex-wrap gap-2">
        {chat && (
          <Link to="/agents" className={buttonVariants({ size: "sm" })}>
            Start a chat
          </Link>
        )}
        <Button variant="ghost" size="sm" onClick={onDone}>
          Done
        </Button>
      </div>
    </section>
  )
}
