/** PROTOTYPE for docs/plans/host-defaults.md: the Services page, drawn from
 * fixed data so its states can be screenshotted. Nothing here calls the API.
 * `?welcome=1` shows the first sign-in header, `?own=<key>` opens the key
 * form on that row. */

import { useState } from "react"
import { Link } from "@tanstack/react-router"
import { KeyRoundIcon } from "lucide-react"

import { DocPage } from "@/components/identity/page-layout"
import { PageHeader } from "@/components/identity/page-header"
import { Pill, type PillTone } from "@/components/identity/pill"
import { ProviderLogo } from "@/components/providers/provider-marks"
import {
  SettingRow,
  SettingsSection,
} from "@/components/settings-page/setting-row"
import { Button, buttonVariants } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { providerInfo } from "@/lib/actor-identity"

const HOST = "Crane"

type Row = {
  key: string
  name: string
  what: string
  ready: [PillTone, string]
  isNew?: boolean
  placeholder?: string
  /** A label for the own-key action where it is not a key form. */
  own?: string
}

const PROVIDED: Row[] = [
  {
    key: "openai",
    name: "OpenAI",
    what: "Chat models for your agents: gpt-5 and gpt-5-mini.",
    ready: ["ok", "Ready"],
    placeholder: "sk-…",
  },
  {
    key: "anthropic",
    name: "Anthropic",
    what: "Claude models for your agents: Opus 5.5, Sonnet 5.5 and Haiku 5.5.",
    ready: ["ok", "Ready"],
    placeholder: "sk-ant-…",
  },
  {
    key: "embeddings",
    name: "Search by meaning",
    what: "Lets search find your notes and messages by what they mean, not only by the words in them. Uses OpenAI embeddings.",
    ready: ["ok", "Ready"],
    own: "Use your own…",
  },
  {
    key: "firecrawl",
    name: "Web search and reading",
    what: "Lets agents search the web and read pages, through Firecrawl.",
    ready: ["ok", "Ready"],
    isNew: true,
    placeholder: "fc-…",
  },
]

function param(name: string): string | null {
  return new URLSearchParams(window.location.search).get(name)
}

function Title({ row }: { row: { key: string; name: string; isNew?: boolean } }) {
  return (
    <span className="flex items-center gap-2.5">
      <ProviderLogo info={providerInfo(row.key)} />
      <span>{row.name}</span>
      {row.isNew && (
        <Pill tone="accent" dot={false}>
          New
        </Pill>
      )}
    </span>
  )
}

function Allowance() {
  const used = 0.74
  const total = 2
  const pct = Math.round((used / total) * 100)
  return (
    <SettingsSection title="Today's allowance" hint={`paid by ${HOST}`}>
      <div className="px-4 py-3.5">
        <div className="flex flex-wrap items-baseline justify-between gap-2">
          <div>
            <span className="text-[22px] font-semibold">${used.toFixed(2)}</span>
            <span className="text-muted-foreground"> of ${total.toFixed(2)} used</span>
          </div>
          <div className="text-[12.5px] text-faint">Resets at 01:00, in 9 hours</div>
        </div>
        <div className="mt-2 h-2 w-full overflow-hidden rounded-full bg-hover">
          <div className="h-full bg-primary" style={{ width: `${pct}%` }} />
        </div>
        <div className="mt-2 max-w-[70ch] text-[12.5px] text-faint">
          Shared by everything {HOST} pays for below: chat $0.61, search
          embeddings $0.02, web search and reading $0.11. When it runs out,
          those services stop until it resets. Services on your own key are
          not counted.
        </div>
      </div>
    </SettingsSection>
  )
}

function ProvidedRow({ row, open }: { row: Row; open: boolean }) {
  const [editing, setEditing] = useState(open)
  return (
    <SettingRow
      title={<Title row={row} />}
      description={
        <>
          {row.what}
          <div className="mt-1 text-muted-foreground">Key: provided by {HOST}</div>
        </>
      }
      control={
        <>
          <Pill tone={row.ready[0]}>{row.ready[1]}</Pill>
          {!editing && (
            <Button variant="outline" size="sm" onClick={() => setEditing(true)}>
              <KeyRoundIcon /> {row.own ?? "Use your own key"}
            </Button>
          )}
        </>
      }
    >
      {editing && (
        <div className="rounded-lg border border-border bg-panel p-3.5">
          <label className="text-[12.5px] font-medium" htmlFor={`k-${row.key}`}>
            Your {row.name} API key
          </label>
          <div className="mt-1.5 flex flex-wrap gap-2">
            <Input
              id={`k-${row.key}`}
              type="password"
              placeholder={row.placeholder}
              defaultValue="sk-ant-api03-4f9c2e81b7d04a6e9d1c"
              className="max-w-[360px] font-mono"
            />
            <Button size="sm">Save key</Button>
            <Button variant="ghost" size="sm" onClick={() => setEditing(false)}>
              Cancel
            </Button>
          </div>
          <p className="mt-2 max-w-[62ch] text-[12.5px] text-faint">
            From now on your agents call {row.name} with your key, billed to
            your account, and can use any of its models. Those calls no
            longer count against {HOST}&rsquo;s allowance. The key is stored encrypted in
            your repository and never shown again. You can switch back to{" "}
            {HOST}&rsquo;s at any time.
          </p>
        </div>
      )}
    </SettingRow>
  )
}

export function ServicesPage() {
  const welcome = param("welcome") === "1"
  const own = param("own")
  return (
    <DocPage className="pb-20">
      {welcome ? (
        <div className="mt-6 rounded-[12px] border border-border bg-panel px-6 py-5">
          <div className="text-[13px] font-medium text-primary-text">
            Welcome to {HOST}
          </div>
          <h1 className="mt-1 font-heading text-[26px] leading-tight font-semibold">
            Your agents can start working now
          </h1>
          <p className="mt-2 max-w-[64ch] text-muted-foreground">
            {HOST} pays for the AI models, search and web reading below, up to
            a daily allowance, so you can chat with your agents today. Your own
            accounts are yours to connect. Anywhere you have your own key, add
            it and it is used instead of {HOST}&rsquo;s.
          </p>
          <ol className="mt-4 grid gap-2 text-[13.5px] sm:grid-cols-3">
            <li className="rounded-lg border border-border bg-background px-3 py-2.5">
              <div className="flex items-center gap-2">
                <Pill tone="ok">Ready</Pill>
                <span className="font-medium">Chat and search</span>
              </div>
              <div className="mt-1 text-faint">Paid by {HOST}. Nothing to set up.</div>
            </li>
            <li className="rounded-lg border border-border bg-background px-3 py-2.5">
              <div className="flex items-center gap-2">
                <Pill tone="warn">Needs you</Pill>
                <span className="font-medium">Connect Google</span>
              </div>
              <div className="mt-1 text-faint">One approval brings in mail and calendars.</div>
            </li>
            <li className="rounded-lg border border-border bg-background px-3 py-2.5">
              <div className="flex items-center gap-2">
                <Pill tone="neutral" dot={false}>Optional</Pill>
                <span className="font-medium">Your own keys</span>
              </div>
              <div className="mt-1 text-faint">Replace any of {HOST}&rsquo;s below.</div>
            </li>
          </ol>
          <div className="mt-4 flex gap-2">
            <Link to="/agents" className={buttonVariants({ size: "sm" })}>
              Start a chat
            </Link>
            <Button variant="ghost" size="sm">
              Done
            </Button>
          </div>
        </div>
      ) : (
        <PageHeader
          title="Services"
          description={`The outside services your substrate uses, where each one's key comes from, and whether it works. ${HOST} provides some of them; your own key always wins.`}
        />
      )}

      <Allowance />

      <SettingsSection
        title={`Provided by ${HOST}`}
        hint="your own key replaces any of these"
      >
        {PROVIDED.map((r) => (
          <ProvidedRow key={r.key} row={r} open={own === r.key} />
        ))}
      </SettingsSection>

      <SettingsSection title="Using your own key">
        <SettingRow
          title={<Title row={{ key: "gemini", name: "Gemini" }} />}
          description={
            <>
              Gemini models for your agents. {HOST} does not provide this
              one.
              <div className="mt-1 text-destructive">
                The last call was refused: 401, API key not valid.
              </div>
            </>
          }
          control={
            <>
              <Pill tone="bad">Key refused</Pill>
              <Button variant="outline" size="sm">
                Replace key
              </Button>
            </>
          }
        />
      </SettingsSection>

      <SettingsSection title="Connect your accounts" hint="bring your own data in">
        <SettingRow
          title={<Title row={{ key: "google", name: "Google" }} />}
          description={
            <>
              Contacts, mail, calendars and Drive. {HOST} has registered the
              sign-in with Google, so connecting is one approval.
              <div className="mt-1.5 rounded-md bg-warn-soft px-2.5 py-1.5 text-warning">
                {HOST}&rsquo;s Google sign-in is in testing. Ask {HOST} to add
                your Google address before you connect, and expect to
                reconnect once a week.
              </div>
            </>
          }
          control={
            <>
              <Pill tone="warn">Not connected</Pill>
              <Button size="sm">Connect</Button>
            </>
          }
        />
        <SettingRow
          title={<Title row={{ key: "linear", name: "Linear" }} />}
          description="Your issues and projects. Needs a personal API key from your Linear settings."
          control={
            <Button variant="outline" size="sm">
              Set up
            </Button>
          }
        />
      </SettingsSection>

      <SettingsSection title="Who sees what">
        <div className="px-4 py-3.5 text-[13.5px] text-muted-foreground">
          Calls {HOST} pays for are made with {HOST}&rsquo;s accounts at
          OpenAI, Anthropic and Firecrawl and billed to {HOST}. With your own
          key they are billed to your account instead. Either way this server
          runs your agents, so its operator can see the requests it makes; your
          own key changes who pays, not who runs the server.
        </div>
      </SettingsSection>
    </DocPage>
  )
}
