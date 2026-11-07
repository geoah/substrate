/** The welcome at the top of Providers, shown where the deployment loaded an
 * operator defaults file (`operatorDefaults` in server.json) until the
 * reader dismisses it: what works now, the accounts that wait on the
 * reader, and that any of it can be their own. It says what works, never who
 * pays for it: a reader new to the substrate has no use for the keys. Registration
 * lands here on such a deployment. **Done** dismisses it for every browser,
 * because the fact lives on the console preference record. */

import type { ReactNode } from "react"
import { Link } from "@tanstack/react-router"

import { Pill } from "@/components/identity/pill"
import { pageTitleClass } from "@/components/identity/page-title"
import { Button, buttonVariants } from "@/components/ui/button"
import type { OperatorAllowance, OperatorDefault } from "@/lib/api/types"
import {
  centsWords,
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
  /** What the operator pays for in a day, where it set a limit. */
  allowance?: OperatorAllowance
  onDone: () => void
}) {
  const { works, connect } = welcomeFacts(items)
  const chat = works.includes("chat")
  const one = works.length === 1
  const limit = allowance
    ? `, within a daily allowance of ${centsWords(allowance.centsPerDay)}`
    : ""
  return (
    <section
      aria-labelledby="providers-welcome"
      className="rounded-[12px] border border-border bg-panel px-6 py-5"
    >
      <div className="text-[13px] font-medium text-primary-text">
        {words.named ? `Welcome to ${words.name}` : "Welcome"}
      </div>
      <h1 id="providers-welcome" className={cn(pageTitleClass("page"), "mt-1")}>
        {chat ? "Your agents are ready to go" : "Here’s what’s ready for you"}
      </h1>
      <p className="mt-2 max-w-[100ch] text-muted-foreground">
        {works.length > 0
          ? `${sentenceCase(andList(works))} ${one ? "is" : "are"} already set up, so you can try ${one ? "it" : "them"} right away${limit}. `
          : ""}
        Connect accounts like Google or WHOOP to bring in your email, calendar
        and other data. If you’d rather use your own AI accounts, you can switch
        at any time under AI models, further down this page.
      </p>
      <ol className="mt-4 grid gap-2 text-[13.5px] sm:grid-cols-3">
        {works.length > 0 && (
          <Step
            pill={<Pill tone="ok">On</Pill>}
            title={sentenceCase(andList(works))}
          >
            Ready now. Nothing to set up.
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
            ? "Sign in once, and your data comes in and stays up to date."
            : "Bring in your email, calendar and more, under Accounts you connect."}
        </Step>
        <Step
          pill={
            <Pill tone="neutral" dot={false}>
              Optional
            </Pill>
          }
          title="Use your own AI accounts"
        >
          Have an OpenAI, Anthropic or Gemini account? Add it under AI models,
          and your agents use it instead.
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
