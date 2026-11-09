import {
  SidebarItem,
  Spinner,
  TabBar,
  TabItem,
  useMediaQuery,
} from "@substrate/react"
import {
  Activity,
  CircleAlert,
  Database,
  FileHeart,
  FlaskConical,
  Settings,
} from "lucide-react"
import { useEffect, type ReactNode } from "react"
import { useBackend } from "./data"
import { go, sectionOf, useRoute, type Route, type Section } from "./route"
import { GettingStartedScreen } from "./screens/GettingStartedScreen"
import { HealthspanMetricScreen } from "./screens/HealthspanMetricScreen"
import { HealthspanScreen } from "./screens/HealthspanScreen"
import { HowScreen } from "./screens/HowScreen"
import { ImportScreen } from "./screens/ImportScreen"
import { LabScreen } from "./screens/LabScreen"
import { LabsScreen } from "./screens/LabsScreen"
import { RecordScreen } from "./screens/RecordScreen"
import { ReportScreen } from "./screens/ReportScreen"
import { ReportsScreen } from "./screens/ReportsScreen"
import { SettingsScreen } from "./screens/SettingsScreen"
import { SourceScreen } from "./screens/SourceScreen"
import { SourcesScreen } from "./screens/SourcesScreen"
import { TodayScreen } from "./screens/TodayScreen"
import { ShellContext } from "./ui/shell"

interface Item {
  section: Section
  label: string
  icon: ReactNode
  route: Route
}

const icon = "size-5"

function items(hasRecord: boolean): Item[] {
  return [
    {
      section: "today",
      label: "Today",
      icon: <CircleAlert className={icon} aria-hidden />,
      route: { screen: "today" },
    },
    {
      section: "labs",
      label: "Labs",
      icon: <FlaskConical className={icon} aria-hidden />,
      route: { screen: "labs", query: "" },
    },
    {
      section: "healthspan",
      label: "Healthspan",
      icon: <Activity className={icon} aria-hidden />,
      route: { screen: "healthspan", query: "" },
    },
    ...(hasRecord
      ? [
          {
            section: "record" as const,
            label: "Record",
            icon: <FileHeart className={icon} aria-hidden />,
            route: { screen: "record", query: "" } as Route,
          },
        ]
      : []),
    {
      section: "sources",
      label: "Sources",
      icon: <Database className={icon} aria-hidden />,
      route: { screen: "sources" },
    },
  ]
}

const settingsItem: Item = {
  section: "settings",
  label: "Settings",
  icon: <Settings className={icon} aria-hidden />,
  route: { screen: "settings" },
}

/** The app: a tab per section on a phone (Settings from each screen's
 * bar), the sections in a sidebar from 768 px. Getting started opens first
 * until the owner finishes or skips it. */
export function App() {
  const route = useRoute()
  const wide = useMediaQuery("(min-width: 768px)")
  const backend = useBackend()
  const gate = backend.profile.useGate()
  const hasRecord = backend.record.useHasRecord()
  const section = sectionOf(route)

  useEffect(() => {
    if (gate.ready && gate.needsStart && route.screen !== "start")
      go({ screen: "start" }, true)
  }, [gate.ready, gate.needsStart, route.screen])

  if (!gate.ready) {
    return (
      <div className="flex min-h-full items-center justify-center bg-grouped text-secondary">
        <Spinner />
      </div>
    )
  }

  const screen = <ScreenFor route={route} />
  if (route.screen === "start") {
    return (
      <ShellContext.Provider value={{ wide: false }}>
        <main className="min-h-full bg-grouped">{screen}</main>
      </ShellContext.Provider>
    )
  }

  const sections = items(hasRecord)
  if (!wide) {
    return (
      <ShellContext.Provider value={{ wide: false }}>
        <div className="min-h-full bg-grouped">
          <main>{screen}</main>
          <TabBar>
            {sections.map((it) => (
              <TabItem
                key={it.section}
                icon={it.icon}
                label={it.label}
                active={section === it.section}
                onClick={() => go(it.route)}
              />
            ))}
          </TabBar>
        </div>
      </ShellContext.Provider>
    )
  }

  return (
    <ShellContext.Provider value={{ wide: true }}>
      <div className="flex h-full overflow-hidden">
        <aside className="flex h-full w-[240px] shrink-0 flex-col gap-1 overflow-y-auto border-r border-separator/50 bg-sidebar p-3">
          <p className="px-3 pt-2 pb-3 text-[22px] font-bold tracking-tight">
            Healthspan
          </p>
          {[...sections, settingsItem].map((it) => (
            <SidebarItem
              key={it.section}
              icon={it.icon}
              label={it.label}
              active={section === it.section}
              onClick={() => go(it.route)}
            />
          ))}
        </aside>
        <main className="h-full min-w-0 flex-1 overflow-y-auto bg-grouped">
          {screen}
        </main>
      </div>
    </ShellContext.Provider>
  )
}

function ScreenFor({ route }: { route: Route }) {
  switch (route.screen) {
    case "today":
      return <TodayScreen />
    case "labs":
      return <LabsScreen />
    case "lab":
      return <LabScreen entry={route.entry} query={route.query} />
    case "reports":
      return <ReportsScreen />
    case "report":
      return <ReportScreen id={route.id} />
    case "healthspan":
      return <HealthspanScreen />
    case "healthspanMetric":
      return <HealthspanMetricScreen id={route.id} />
    case "how":
      return <HowScreen />
    case "record":
      return <RecordScreen />
    case "sources":
      return <SourcesScreen />
    case "source":
      return <SourceScreen id={route.id} />
    case "import":
      return <ImportScreen id={route.id} />
    case "settings":
      return <SettingsScreen />
    case "start":
      return <GettingStartedScreen />
  }
}
