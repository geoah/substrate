/** Settings (`/settings`): how the console looks (saved on the repository's
 * console preference record, so every browser signed in looks the same),
 * what every agent may spend in a day, the owner's own contact, the account
 * and the proof of its name, where you are signed in,
 * the apps you approved, the way to the AI assistants page, a download of
 * everything, and for developers the system kinds, the raw tokens and what
 * this server says about itself. */

import { useState } from "react"
import { useMutation, useQuery } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import { CodeIcon, DownloadIcon } from "lucide-react"

import { DocPage } from "@/components/identity/page-layout"
import { PageHeader } from "@/components/identity/page-header"
import { ToggleSwitch } from "@/components/nav/toggle-switch"
import {
  SettingRow,
  SettingsSection,
  WidthPicker,
} from "@/components/settings-page/setting-row"
import { OwnerContactRow } from "@/components/settings-page/owner-contact-row"
import { SpendCapRow } from "@/components/settings-page/spend-cap-row"
import { VerifyDomainRow } from "@/components/settings-page/verify-domain-row"
import { Button, buttonVariants } from "@/components/ui/button"
import { Segmented } from "@/components/ui/segmented"
import { Spinner } from "@/components/ui/spinner"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { toast } from "@/components/ui/toast"
import {
  useConsolePreferences,
  useDensity,
  useLayoutWidths,
  useTechnicalDetails,
} from "@/hooks/use-console-preferences"
import { tokensQueryOptions } from "@/lib/api/auth"
import { downloadExport } from "@/lib/api/export"
import { discoveryQueryOptions } from "@/lib/api/discovery"
import { CORE_AUTHORITY, olderServerMessage } from "@/lib/api/http"
import { assistantAddress, isAssistantToken } from "@/lib/assistants"
import { isMachineryKind } from "@/lib/core-kinds"
import { kindsQueryOptions } from "@/lib/api/kinds"
import {
  CONSOLE_PREFERENCE_KIND,
  type ThemePreference,
} from "@/lib/console-preferences"
import { changeServer, checkForUpdates, nativeShell } from "@/lib/shell"
import {
  AccountRows,
  DeleteRepositoryRow,
  SignOutEverywhereRow,
} from "@/pages/account"
import { ApiTokens, AppTokenRows, SignedInRows } from "@/pages/tokens"
import { ExternalLink } from "@/components/external-link"

const RECORD_WIDTHS = [
  { value: "narrow", label: "Narrow", fill: "40%" },
  { value: "wide", label: "Wide", fill: "65%" },
  { value: "full", label: "Full", fill: "100%" },
] as const

const TABLE_WIDTHS = [
  { value: "wide", label: "Wide", fill: "75%" },
  { value: "full", label: "Full", fill: "100%" },
] as const

const DENSITIES = [
  { value: "comfortable", label: "Comfortable" },
  { value: "compact", label: "Compact" },
] as const

const THEMES = [
  { value: "system", label: "System" },
  { value: "light", label: "Light" },
  { value: "dark", label: "Dark" },
] as const

function LayoutRows() {
  const { preferences, busy, set } = useConsolePreferences()
  const widths = useLayoutWidths()
  const [density, setDensity] = useDensity()
  const [technical, setTechnical] = useTechnicalDetails()
  return (
    <>
      <SettingRow
        title="Record pages"
        description="How wide a record, a tool or a provider page is. Pages start at the left edge."
        control={
          <WidthPicker
            label="Record page width"
            value={widths.recordWidth}
            options={RECORD_WIDTHS}
            onChange={widths.setRecordWidth}
            disabled={busy}
          />
        }
      />
      <SettingRow
        title="Tables"
        description="Full width uses the whole window; wide keeps long lines readable on big screens."
        control={
          <WidthPicker
            label="Table width"
            value={widths.tableWidth}
            options={TABLE_WIDTHS}
            onChange={widths.setTableWidth}
            disabled={busy}
          />
        }
      />
      <SettingRow
        title="Table rows"
        description="How tall each row is in a collection’s table and in All data."
        control={
          <Segmented
            label="Table row height"
            value={density}
            options={DENSITIES}
            onChange={setDensity}
            disabled={busy}
          />
        }
      />
      <SettingRow
        title="Technical details"
        description="Shows ids, full references, who wrote what, and change numbers. For developers."
        control={
          <ToggleSwitch
            checked={technical}
            onChange={setTechnical}
            label="Show technical details"
            disabled={busy}
          />
        }
      />
      <SettingRow
        title="Appearance"
        control={
          <Segmented
            label="Appearance"
            value={preferences.theme}
            options={THEMES}
            onChange={(theme: ThemePreference) => set("theme", theme)}
            disabled={busy}
          />
        }
      />
    </>
  )
}

/** The way to the AI assistants page, saying how many are connected and,
 * in three lines, how one connects: the page has each client's commands. */
export function AssistantsRow() {
  const tokens = useQuery(tokensQueryOptions)
  const connected = (tokens.data ?? []).filter(isAssistantToken).length
  return (
    <SettingRow
      title="Claude Code, Codex, Cursor and others"
      description={
        connected === 0
          ? "Let an AI assistant work with your data."
          : `${connected} connected. Open the page to see them or revoke one.`
      }
      control={
        <Button
          variant="outline"
          size="sm"
          nativeButton={false}
          render={<Link to="/settings/assistants" />}
        >
          Open
        </Button>
      }
    >
      <ol
        aria-label="How to connect an assistant"
        className="flex list-decimal flex-col gap-1 pl-5 text-[13px] text-muted-foreground"
      >
        <li>
          Give your assistant this address:{" "}
          <code className="font-mono text-[12px] [overflow-wrap:anywhere] text-foreground">
            {assistantAddress()}
          </code>
        </li>
        <li>When your browser opens this console, approve it.</li>
        <li>
          Open shows the exact steps for Claude Code, Codex, Claude and Cursor.
        </li>
      </ol>
    </SettingRow>
  )
}

function DownloadRow() {
  const download = useMutation({
    mutationFn: downloadExport,
    onSuccess: (name) =>
      toast.add({ type: "success", title: `Downloading ${name}.` }),
    onError: (error) =>
      toast.add({
        type: "error",
        title: "The download didn’t start",
        description:
          olderServerMessage(error, "make this download") ?? error.message,
      }),
  })
  return (
    <SettingRow
      title="Download everything"
      description="One file with all your data and its history, as of now."
      control={
        <Button
          variant="outline"
          size="sm"
          disabled={download.isPending}
          onClick={() => download.mutate()}
        >
          {download.isPending ? <Spinner /> : <DownloadIcon />}
          Download
        </Button>
      }
    />
  )
}

/** What this server says about itself in its discovery file: its version,
 * where its API is, and the features it lists. A feature is a group of
 * routes; its three facts sit under their own column headers, and the notice
 * column is drawn only where a feature carries one. A server older than the
 * revision count sends none, which reads as a dash and never as a number. */
export function ServerRows() {
  const server = useQuery(discoveryQueryOptions)
  const doc = server.data
  const features = doc?.features ?? []
  const notices = features.some((f) => f.notice)
  return (
    <SettingRow
      title="This server"
      description={
        server.isError
          ? `It didn’t answer: ${server.error.message}`
          : doc
            ? `Version ${doc.server?.version ?? "unknown"} · API at ${doc.surfaces?.rest?.endpoint ?? "/api/v1"}`
            : "Asking…"
      }
    >
      {features.length > 0 ? (
        <div className="flex flex-col gap-2">
          <p className="max-w-[68ch] text-[12.5px] text-faint">
            Each feature is a group of routes. Its stability says how far its
            shape is promised, and its revision goes up when it gains something.
          </p>
          <div className="overflow-x-auto rounded-lg border border-border">
            <Table aria-label="Features">
              <TableHeader>
                <TableRow>
                  <TableHead>Feature</TableHead>
                  <TableHead>Stability</TableHead>
                  <TableHead className="text-right">Revision</TableHead>
                  {notices && <TableHead>Notice</TableHead>}
                </TableRow>
              </TableHeader>
              <TableBody>
                {features.map((f) => (
                  <TableRow key={f.name}>
                    <TableCell className="font-mono text-xs">
                      {f.name}
                    </TableCell>
                    <TableCell className="text-muted-foreground">
                      {f.stability}
                    </TableCell>
                    <TableCell className="text-right text-muted-foreground tabular-nums">
                      {typeof f.revision === "number" ? f.revision : "—"}
                    </TableCell>
                    {notices && (
                      <TableCell className="min-w-[16rem] whitespace-normal text-muted-foreground">
                        {f.notice}
                      </TableCell>
                    )}
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        </div>
      ) : undefined}
    </SettingRow>
  )
}

/** In a native app the substrate is the one the app was pointed at, and this
 * is the way to point it elsewhere; a browser's address bar already is. */
function ShellServerRow() {
  const shell = nativeShell()
  if (!shell) return null
  return (
    <SettingRow
      title="Server"
      description={
        <span className="font-mono [overflow-wrap:anywhere]">
          {shell.server}
        </span>
      }
      control={
        <Button variant="outline" size="sm" onClick={() => changeServer()}>
          Change server
        </Button>
      }
    />
  )
}

/** The native app's own version, and the check for a newer one: the app
 * checks by itself at launch, and this is the way to ask now. */
function ShellAppRow() {
  const shell = nativeShell()
  if (!shell?.version) return null
  return (
    <SettingRow
      title="App version"
      description={
        <span className="font-mono [overflow-wrap:anywhere]">
          {shell.version}
        </span>
      }
      control={
        shell.updates ? (
          <Button variant="outline" size="sm" onClick={() => checkForUpdates()}>
            Check for updates
          </Button>
        ) : undefined
      }
    />
  )
}

/** The app's privacy policy, as its release names it: in the browser. */
function ShellPrivacyRow() {
  const shell = nativeShell()
  if (!shell?.privacyPolicy) return null
  return (
    <SettingRow
      title="Privacy policy"
      description="What the app does with your data."
      control={
        <ExternalLink
          href={shell.privacyPolicy}
          className={buttonVariants({ variant: "outline", size: "sm" })}
        >
          Open
        </ExternalLink>
      }
    />
  )
}

function DeveloperRows() {
  const kinds = useQuery(kindsQueryOptions)
  const system = (kinds.data ?? []).filter((k) =>
    isMachineryKind(k.identity)
  ).length
  const [tokens, setTokens] = useState(false)
  return (
    <>
      <SettingRow
        title="System kinds"
        description={`The ${system || "built-in"} kinds of ${CORE_AUTHORITY}'s core, llm and console packages: the substrate's own machinery.`}
        control={
          <Button
            variant="outline"
            size="sm"
            nativeButton={false}
            render={
              <Link
                to="/data/$authority"
                params={{ authority: CORE_AUTHORITY }}
              />
            }
          >
            Browse
          </Button>
        }
      />
      <SettingRow
        title="API tokens"
        description="Every signed-in browser and script is a token record with full access to your repository. Mint one for a script or a device. Apps you approved hold narrower tokens, listed under Apps."
        control={
          <Button
            variant="outline"
            size="sm"
            aria-expanded={tokens}
            onClick={() => setTokens((v) => !v)}
          >
            {tokens ? "Hide" : "Manage"}
          </Button>
        }
      >
        {tokens && <ApiTokens />}
      </SettingRow>
      <ServerRows />
    </>
  )
}

export function ConsoleSettingsPage() {
  const [technical] = useTechnicalDetails()
  return (
    <DocPage className="pb-20">
      <PageHeader title="Settings" />
      <SettingsSection
        title="Layout"
        hint="saved in your substrate, so every browser you sign in from looks the same"
      >
        <LayoutRows />
      </SettingsSection>
      {technical && (
        <div className="mt-2.5 flex flex-col gap-1.5 rounded-lg border border-dashed border-border-strong px-3 py-2.5 text-[12.5px] text-muted-foreground">
          <div className="flex items-center gap-1.5 text-[11.5px] tracking-[0.05em] text-faint uppercase">
            <CodeIcon className="size-3.5" />
            Developer
          </div>
          <div>
            Stored on{" "}
            <span className="font-mono [overflow-wrap:anywhere]">
              {CONSOLE_PREFERENCE_KIND}/navigation
            </span>
            , beside the sidebar state it keeps (
            <span className="font-mono">collapsed</span>,{" "}
            <span className="font-mono">favorites</span>). Whether the sidebar
            is open stays in this browser.
          </div>
        </div>
      )}
      <SettingsSection title="Agents" id="agents">
        <SpendCapRow />
      </SettingsSection>
      <SettingsSection title="Account">
        <OwnerContactRow />
        <AccountRows afterName={<VerifyDomainRow />} />
      </SettingsSection>
      <SettingsSection title="Signed in">
        <ShellServerRow />
        <ShellAppRow />
        <ShellPrivacyRow />
        <SignedInRows />
        <SignOutEverywhereRow />
      </SettingsSection>
      <SettingsSection
        title="Apps"
        hint="apps you approved; each reaches only the records you let it"
      >
        <AppTokenRows />
      </SettingsSection>
      <SettingsSection title="AI assistants">
        <AssistantsRow />
      </SettingsSection>
      <SettingsSection title="Your data">
        <DownloadRow />
      </SettingsSection>
      {technical && (
        <SettingsSection title="Developer">
          <DeveloperRows />
        </SettingsSection>
      )}
      <SettingsSection
        title="Danger zone"
        hint="nothing here can be undone"
        id="danger"
        danger
      >
        <DeleteRepositoryRow />
      </SettingsSection>
    </DocPage>
  )
}
