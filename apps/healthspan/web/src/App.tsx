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
import { Suspense, lazy, useEffect, type ReactNode } from "react"
import { useBackend } from "./data"
import { go, sectionOf, useRoute, type Route, type Section } from "./route"
import { ShellContext } from "./ui/shell"

// Each screen loads on its first visit, so the first paint carries the
// shell and the screen it opens on, not every chart and the import.
const GettingStartedScreen = lazy(() =>
  import("./screens/GettingStartedScreen").then((m) => ({
    default: m.GettingStartedScreen,
  }))
)
const HealthspanScreen = lazy(() =>
  import("./screens/HealthspanScreen").then((m) => ({
    default: m.HealthspanScreen,
  }))
)
const HealthspanMetricScreen = lazy(() =>
  import("./screens/HealthspanMetricScreen").then((m) => ({
    default: m.HealthspanMetricScreen,
  }))
)
const HowScreen = lazy(() =>
  import("./screens/HowScreen").then((m) => ({ default: m.HowScreen }))
)
const ImportScreen = lazy(() =>
  import("./screens/ImportScreen").then((m) => ({ default: m.ImportScreen }))
)
const LabScreen = lazy(() =>
  import("./screens/LabScreen").then((m) => ({ default: m.LabScreen }))
)
const LabsScreen = lazy(() =>
  import("./screens/LabsScreen").then((m) => ({ default: m.LabsScreen }))
)
const RecordScreen = lazy(() =>
  import("./screens/RecordScreen").then((m) => ({ default: m.RecordScreen }))
)
const ReportScreen = lazy(() =>
  import("./screens/ReportScreen").then((m) => ({ default: m.ReportScreen }))
)
const ReportsScreen = lazy(() =>
  import("./screens/ReportsScreen").then((m) => ({ default: m.ReportsScreen }))
)
const SettingsScreen = lazy(() =>
  import("./screens/SettingsScreen").then((m) => ({
    default: m.SettingsScreen,
  }))
)
const SourceScreen = lazy(() =>
  import("./screens/SourceScreen").then((m) => ({ default: m.SourceScreen }))
)
const SourcesScreen = lazy(() =>
  import("./screens/SourcesScreen").then((m) => ({ default: m.SourcesScreen }))
)
const TodayScreen = lazy(() =>
  import("./screens/TodayScreen").then((m) => ({ default: m.TodayScreen }))
)

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

  const screen = (
    <Suspense
      fallback={
        <div className="flex justify-center py-24 text-secondary">
          <Spinner />
        </div>
      }
    >
      <ScreenFor route={route} />
    </Suspense>
  )
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
