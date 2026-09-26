/** Settings (`/settings`): how the console looks (saved on the repository's
 * console preference record, so every browser signed in looks the same),
 * the account, where you are signed in, a download of everything, and for
 * developers the system kinds, the raw tokens and what this server says
 * about itself. */

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
import { Button } from "@/components/ui/button"
import { Segmented } from "@/components/ui/segmented"
import { Spinner } from "@/components/ui/spinner"
import { toast } from "@/components/ui/toast"
import {
  useConsolePreferences,
  useDensity,
  useLayoutWidths,
  useTechnicalDetails,
} from "@/hooks/use-console-preferences"
import { downloadExport } from "@/lib/api/export"
import { CORE_AUTHORITY, olderServerMessage, request } from "@/lib/api/http"
import { kindsQueryOptions } from "@/lib/api/kinds"
import {
  CONSOLE_PREFERENCE_KIND,
  type ThemePreference,
} from "@/lib/console-preferences"
import { AccountRows } from "@/pages/account"
import { ApiTokens, SignedInRows } from "@/pages/tokens"

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
        title="Row height"
        control={
          <Segmented
            label="Row height"
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

/** What `GET /.well-known/substrate/server.json` says; the page reads a few
 * fields and shows them, so nothing here is a wire contract of its own. */
interface ServerDoc {
  server?: { version?: string }
  features?: { name: string; stability: string }[]
  surfaces?: Record<string, { endpoint?: string }>
}

function ServerRows() {
  const server = useQuery({
    queryKey: ["discovery", "server"],
    queryFn: () =>
      request<ServerDoc>(
        "GET",
        "/.well-known/substrate/server.json",
        undefined,
        { anonymous: true }
      ),
    staleTime: 5 * 60_000,
  })
  const doc = server.data
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
      {doc?.features?.length ? (
        <ul className="flex flex-wrap gap-1.5">
          {doc.features.map((f) => (
            <li
              key={f.name}
              className="rounded-full border border-border px-2 py-px text-xs text-muted-foreground"
            >
              {f.name}
              {f.stability !== "stable" && (
                <span className="text-faint"> · {f.stability}</span>
              )}
            </li>
          ))}
        </ul>
      ) : undefined}
    </SettingRow>
  )
}

function DeveloperRows() {
  const kinds = useQuery(kindsQueryOptions)
  const system = (kinds.data ?? []).filter(
    (k) => k.authority === CORE_AUTHORITY
  ).length
  const [tokens, setTokens] = useState(false)
  return (
    <>
      <SettingRow
        title="System kinds"
        description={`The ${system || "built-in"} kinds under ${CORE_AUTHORITY}: the substrate's own machinery.`}
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
        description="Every signed-in browser and script is a token record. Mint one for a script or a device; each has full access."
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
          <div className="flex items-center gap-1.5 text-[11px] tracking-[0.05em] text-faint uppercase">
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
      <SettingsSection title="Account">
        <AccountRows />
      </SettingsSection>
      <SettingsSection title="Signed in">
        <SignedInRows />
      </SettingsSection>
      <SettingsSection title="Your data">
        <DownloadRow />
      </SettingsSection>
      {technical && (
        <SettingsSection title="Developer">
          <DeveloperRows />
        </SettingsSection>
      )}
    </DocPage>
  )
}
