// Renders the console in a real browser against a running substrate. jsdom
// cannot make this check: #846 bumped nuqs, every page rendered blank with a
// TypeError at boot, and the typecheck, lint, build and vitest all passed.
//
//   node scripts/render-smoke.mjs <base URL>   render the routes below
//   node scripts/render-smoke.mjs --prepare    find the browser, downloading
//                                              Playwright's chromium if needed
//
// The browser is the installed Google Chrome (Playwright's "chrome" channel).
// Where none is installed it is the chromium build Playwright keeps under
// ~/.cache/ms-playwright, which --prepare downloads.
// CONSOLE_RENDER_BROWSER=chromium skips Chrome and uses that build.
//
// A route fails on an uncaught page error, a console error, a same-origin
// response of 400 or more that EXPECTED_RESPONSES does not list, a request
// lost to a network change that no retry answered, or its heading or its
// content not appearing within the timeout. The heading and the content are
// asserted because a non-empty root proves nothing: a redirect and a loading
// view both fill the root.
//
// It does not catch an error in a route it does not visit, in an interaction
// other than the register form, or in a state a fresh repository never
// reaches: a record page, a list of records the user wrote, a connected
// provider. The one state it seeds is measurements: before the three-metric
// timeline it installs the health catalogue and writes a few values of three
// of its metrics, so that check proves the charts draw.
//
// /register creates a repository on every run, so point this at a throwaway
// substrate whose door reads no invite code and no second factor, as
// compose.yaml ships.

import { spawnSync } from "node:child_process"
import { randomBytes } from "node:crypto"
import { createRequire } from "node:module"
import { dirname, join } from "node:path"

import { chromium } from "playwright-core"

const TIMEOUT_MS = 20_000
// A floor after a route's content shows, for the requests that content starts.
// A network-idle wait cannot stand in: History holds the change stream open.
const SETTLE_MS = 1_000
// How long a request lost to a network change has to be answered again: the
// console's query client retries a transport failure twice, 1 s then 2 s
// later.
const RETRY_MS = 5_000

// The 4xx answers a fresh repository gives by design, by exact path. The
// console reads its navigation preference record by id and takes the 404 as
// the defaults.
const EXPECTED_RESPONSES = [
  {
    method: "GET",
    path: "/api/v1/substrate.reamde.dev/core/consolepreference/navigation",
    status: 404,
  },
]

// Chrome's console note on a response of 400 or more. A same-origin note is
// left to the response handler, which judges the same response against
// EXPECTED_RESPONSES; any other note is a console error.
const STATUS_NOTE =
  /^Failed to load resource: the server responded with a status of \d+/
// Chrome aborts every request in flight when the host's network interfaces
// change, which a Docker host's do whenever a container starts or stops (a
// veth pair). Such a request is tracked instead, and fails its route unless
// the console sends it again and gets an answer.
const NETWORK_CHANGED = "net::ERR_NETWORK_CHANGED"
const NETWORK_CHANGED_NOTE =
  /^Failed to load resource: net::ERR_NETWORK_CHANGED/

const BROWSERS = ["chrome", "chromium"]
const browserChoice = process.env.CONSOLE_RENDER_BROWSER || "chrome"
if (!BROWSERS.includes(browserChoice)) {
  die(
    `CONSOLE_RENDER_BROWSER is ${JSON.stringify(browserChoice)}; it takes ${BROWSERS.join(" or ")}`
  )
}

function die(message) {
  console.error(`console:render: ${message}`)
  process.exit(2)
}

function firstLine(text) {
  return String(text).split("\n", 1)[0].trim()
}

function expected(method, path, status) {
  return EXPECTED_RESPONSES.some(
    (answer) =>
      answer.method === method &&
      answer.path === path &&
      answer.status === status
  )
}

/** Chrome where it is installed, else Playwright's chromium. A Chrome that is
 * installed and fails to start is an error, never a reason to fall back. */
async function launch({ download = false } = {}) {
  if (browserChoice === "chrome") {
    try {
      const browser = await chromium.launch({ channel: "chrome" })
      return { browser, name: "Chrome" }
    } catch (err) {
      if (!/distribution 'chrome' is not (found|supported)/.test(err.message)) {
        throw err
      }
    }
  }
  try {
    return { browser: await chromium.launch(), name: "Playwright chromium" }
  } catch (err) {
    if (!err.message.includes("Executable doesn't exist")) throw err
    if (!download) {
      throw new Error(
        "no Chrome is installed and Playwright's chromium is not downloaded: run `mise run console:browser`",
        { cause: err }
      )
    }
  }
  console.log(
    "console:render: no Chrome installed; downloading Playwright's chromium"
  )
  const cli = join(
    dirname(
      createRequire(import.meta.url).resolve("playwright-core/package.json")
    ),
    "cli.js"
  )
  // --no-remove: the default deletes every cached browser no live Playwright
  // install references, and the cache is shared with whatever else is on the
  // machine.
  const install = spawnSync(
    process.execPath,
    [
      cli,
      "install",
      "--only-shell",
      "--no-remove",
      "--no-progress",
      "chromium",
    ],
    { stdio: "inherit" }
  )
  if (install.status !== 0) {
    throw new Error(`playwright-core install exited ${install.status}`)
  }
  return { browser: await chromium.launch(), name: "Playwright chromium" }
}

async function prepare() {
  const { browser, name } = await launch({ download: true })
  console.log(`console:render: will render with ${name} ${browser.version()}`)
  await browser.close()
}

/** Only visible text counts; an empty #root is named only when no title is
 * visible. */
async function screen(page) {
  const path = new URL(page.url()).pathname
  const shown = (
    await page
      .locator('h1, [data-slot="card-title"], [data-slot="empty-title"]')
      .allInnerTexts()
      .catch(() => [])
  )
    .map((text) => text.trim())
    .filter(Boolean)
  if (shown.length > 0) {
    return `${path} shows ${shown.map((text) => JSON.stringify(text)).join(", ")}`
  }
  const empty = await page
    .locator("#root")
    .evaluate((root) => root.childElementCount === 0)
    .catch(() => true)
  return empty ? `${path} has an empty #root` : `${path} shows no heading`
}

/** Only a timeout is reworded; any other error keeps its own message. */
async function expect(page, what, wait) {
  try {
    await wait()
  } catch (err) {
    if (err.name !== "TimeoutError") throw err
    throw new Error(
      `no ${what} within ${TIMEOUT_MS / 1000}s; ${await screen(page)}`,
      { cause: err }
    )
  }
}

function cardTitle(page, text) {
  return page.locator('[data-slot="card-title"]', {
    hasText: new RegExp(`^${text}$`),
  })
}

function pageTitle(page, text) {
  return page.getByRole("heading", { level: 1, name: text, exact: true })
}

async function shows(page, what, locator) {
  await expect(page, what, () => locator.waitFor({ state: "visible" }))
}

async function at(page, path) {
  await expect(page, `redirect to ${path}`, () =>
    page.waitForURL((url) => url.pathname === path)
  )
}

/** The error state ends the wait as soon as it shows, rather than at the
 * timeout. */
async function loaded(page, what, ready, failed) {
  await expect(page, what, () =>
    ready.or(failed).first().waitFor({ state: "visible" })
  )
  if (await failed.first().isVisible()) {
    throw new Error(
      `the page shows ${JSON.stringify(firstLine(await failed.first().innerText()))}`
    )
  }
}

/** The door the form is written against: compose.yaml's, with no invite code
 * and no second factor. Any other door is refused here by name, before the
 * form times out on a field this script does not fill. */
async function openDoor(origin) {
  const res = await fetch(`${origin}/.well-known/substrate/server.json`)
  if (!res.ok) throw new Error(`server.json answered ${res.status}`)
  const { registration = {} } = await res.json()
  if (registration.inviteRequired !== false) {
    throw new Error(
      "this substrate reads an invite code at the register door; the check registers without one"
    )
  }
  if (registration.totpRequired !== false) {
    throw new Error(
      "this substrate requires a second factor at the register door; the check registers without one"
    )
  }
}

/** Installs the health catalogue into the signed-in repository (the
 * observation kinds are seeded) and writes, over the last few hours, heart rate and heart rate
 * variability values (two units, so they share a chart on two axes) and
 * step counts (a third unit, a chart of its own). It runs in the page with
 * the token the register form stored, as the console's own requests do. */
async function seedObservations(page) {
  const problem = await page.evaluate(async () => {
    const token = localStorage.getItem("substrate.token")
    const home = localStorage.getItem("substrate.repository")
    if (!token || !home) return "no signed-in repository to seed"
    const headers = {
      Accept: "application/json",
      Authorization: `Bearer ${token}`,
      "Content-Type": "application/json",
      "X-Substrate-Actor": "console",
    }
    const send = async (method, path, body) => {
      const res = await fetch(path, {
        method,
        headers,
        ...(body ? { body: JSON.stringify(body) } : {}),
      })
      return res.ok ? "" : `${method} ${path} answered ${res.status}`
    }
    const failed = await send(
      "POST",
      `/api/v1/catalog/${encodeURIComponent("substrate.reamde.dev/health")}/install`
    )
    if (failed) return failed
    const kind = "substrate.reamde.dev/observations/observation"
    const metric = (id) => ({
      ref: `substrate.reamde.dev/observations/metric/health/${id}`,
    })
    const hour = 3_600_000
    const now = Date.now()
    for (let i = 1; i <= 6; i++) {
      const at = new Date(now - i * hour)
      const writes = [
        {
          metric: metric("heart_rate"),
          form: "gauge",
          value: { value: String(58 + i), unit: "/min" },
          at: at.toISOString(),
        },
        {
          metric: metric("hrv_rmssd"),
          form: "gauge",
          value: { value: String(40 + i), unit: "ms" },
          at: at.toISOString(),
        },
        {
          metric: metric("steps"),
          form: "amount",
          value: { value: String(300 * i), unit: "{steps}" },
          at: at.toISOString(),
          endsAt: new Date(at.getTime() + hour / 2).toISOString(),
        },
      ]
      for (const properties of writes) {
        const failed = await send("POST", "/api/v1/records", {
          kind,
          properties,
        })
        if (failed) return failed
      }
    }
    return ""
  })
  if (problem) throw new Error(`seeding measurements: ${problem}`)
}

function plural(count, one, many) {
  return `${count} ${count === 1 ? one : many}`
}

const routes = [
  {
    path: "/",
    async check(page) {
      await at(page, "/login")
      await shows(page, '"Sign in" card', cardTitle(page, "Sign in"))
      return 'redirected to /login, "Sign in"'
    },
  },
  {
    path: "/login",
    async check(page) {
      await shows(page, '"Sign in" card', cardTitle(page, "Sign in"))
      return '"Sign in"'
    },
  },
  {
    path: "/register",
    async check(page, origin) {
      await openDoor(origin)
      await shows(page, '"Register" card', cardTitle(page, "Register"))
      const repository = `render-${randomBytes(4).toString("hex")}.example.com`
      const password = randomBytes(12).toString("hex")
      await page.locator("#repository").fill(repository)
      await page.locator("#password").fill(password)
      await page.locator("#confirm").fill(password)
      // The label reads "Create my repository" only once discovery has said
      // there is no second factor; before that the form would buy a seed.
      const create = page.getByRole("button", { name: "Create my repository" })
      await expect(page, '"Create my repository" button', () => create.click())
      await shows(
        page,
        '"Your recovery key" card',
        cardTitle(page, "Your recovery key")
      )
      await page.getByRole("button", { name: "Saved, continue" }).click()
      await at(page, "/")
      await shows(
        page,
        '"Your substrate" heading',
        pageTitle(page, "Your substrate")
      )
      return `registered ${repository}, kept the recovery key, "Your substrate"`
    },
  },
  {
    path: "/data",
    async check(page) {
      await shows(page, '"All data" heading', pageTitle(page, "All data"))
      await loaded(
        page,
        "collections table or empty state",
        page
          .getByRole("columnheader", { name: "Collection", exact: true })
          .or(page.getByText(/^Nothing here yet\./)),
        page.getByText(/^Your collections didn.t load/)
      )
      const rows = await page.locator("tbody tr").count()
      return `"All data", ${rows ? plural(rows, "collection", "collections") : "its empty state"}`
    },
  },
  {
    path: "/history",
    async check(page) {
      await shows(page, '"History" heading', pageTitle(page, "History"))
      const entries = page.locator('[data-slot="history-entry"]')
      await loaded(
        page,
        "change rows or empty state",
        entries.or(page.getByText(/^Nothing has changed yet\./)),
        page.getByText(/^History didn.t load/)
      )
      const rows = await entries.count()
      return `"History", ${rows ? plural(rows, "change", "changes") : "its empty state"}`
    },
  },
  {
    path: "/observations",
    async check(page) {
      await shows(
        page,
        '"Observations" heading',
        pageTitle(page, "Observations")
      )
      // A fresh repository holds the observation kinds but no measurement
      // yet, so its page says where they come from.
      await loaded(
        page,
        "streams table or empty state",
        page
          .getByRole("columnheader", { name: "Metric", exact: true })
          .or(page.getByText(/^Nothing here records measurements yet/)),
        page.getByText(/^The streams didn.t load/)
      )
      const empty = await page
        .getByText(/^Nothing here records measurements yet/)
        .isVisible()
      return `"Observations", ${empty ? "its empty state" : "its streams"}`
    },
  },
  {
    // The table's address and the Timeline panel: a sort, a grouping and
    // the panel open with nothing selected, which says how to draw streams.
    path: "/observations?sort=count:desc&group=metric&timeline=true",
    async check(page) {
      await shows(
        page,
        '"Observations" heading',
        pageTitle(page, "Observations")
      )
      await loaded(
        page,
        "the Timeline panel or empty state",
        page
          .getByRole("region", { name: "Timeline of the selected streams" })
          .or(page.getByText(/^Nothing here records measurements yet/)),
        page.getByText(/^The streams didn.t load/)
      )
      const empty = await page
        .getByText(/^Nothing here records measurements yet/)
        .isVisible()
      return `"Observations", ${empty ? "its empty state" : "grouped, with the Timeline panel"}`
    },
  },
  {
    path: "/observations/timeline",
    async check(page) {
      await shows(page, '"Timeline" heading', pageTitle(page, "Timeline"))
      await loaded(
        page,
        "a chart or empty state",
        page
          .locator('[data-slot="timeline-lane"]')
          .or(page.getByText(/^Nothing here records measurements yet/))
          .or(page.getByText(/^Nothing has been measured yet/)),
        page.getByText(/^This read failed/)
      )
      const lanes = await page.locator('[data-slot="timeline-lane"]').count()
      return `"Timeline", ${lanes ? plural(lanes, "chart", "charts") : "its empty state"}`
    },
  },
  {
    // Three metrics of three units: two overlay on one chart, the left and
    // the right axis, and the third splits into a chart of its own. The
    // repository is seeded before the page opens, so the charts have points
    // to draw and an empty state fails the check; opened first, the page
    // would read metrics no installed package declares yet.
    path: "/observations/timeline?metric=health/heart_rate,health/hrv_rmssd,health/steps",
    before: seedObservations,
    async check(page) {
      await shows(page, '"Timeline" heading', pageTitle(page, "Timeline"))
      const lanes = page.locator('[data-slot="timeline-lane"]')
      await loaded(
        page,
        "two charts",
        lanes.nth(1),
        page
          .getByText(/^This read failed/)
          .or(page.getByText(/^Nothing here records measurements yet/))
          .or(page.getByText(/^Nothing has been measured yet/))
          .or(page.getByText(/^No record names this metric/))
      )
      const count = await lanes.count()
      if (count !== 2)
        throw new Error(`${plural(count, "chart", "charts")}, want 2`)
      // Each lane is named for the metrics it draws.
      const names = await lanes.evaluateAll((all) =>
        all.map((lane) => lane.getAttribute("aria-label") ?? "")
      )
      if (!/^Heart rate and Heart rate variability/.test(names[0] ?? "")) {
        throw new Error(
          `the first chart is ${JSON.stringify(names[0])}, want heart rate and HRV`
        )
      }
      if (names[1] !== "Steps") {
        throw new Error(
          `the second chart is ${JSON.stringify(names[1])}, want Steps`
        )
      }
      // Both draw, and the first reads its second unit on the right.
      await expect(page, "two drawn charts", () =>
        page
          .locator('[data-slot="timeline-chart"]')
          .nth(1)
          .waitFor({ state: "visible" })
      )
      const series = await lanes
        .nth(0)
        .locator("[data-series]")
        .evaluateAll(
          (all) => new Set(all.map((s) => s.getAttribute("data-series"))).size
        )
      if (series < 2) {
        throw new Error(
          `the first chart draws ${plural(series, "series", "series")}, want 2`
        )
      }
      const right = await lanes.nth(0).locator('[data-axis="right"]').count()
      if (right !== 1) {
        throw new Error(
          `the first chart has ${plural(right, "right axis", "right axes")}, want 1`
        )
      }
      return `"Timeline", 2 charts, ${series} series and a right axis on the first`
    },
  },
]

async function render(base) {
  let origin
  try {
    origin = new URL(base).origin
  } catch {
    die(
      `${JSON.stringify(base)} is not a URL; pass the substrate's, such as http://127.0.0.1:8080`
    )
  }

  const { browser, name } = await launch()
  console.log(`console:render: ${name} ${browser.version()} against ${origin}`)
  const page = await (await browser.newContext()).newPage()
  page.setDefaultTimeout(TIMEOUT_MS)

  let problems = []
  // Requests lost to a network change, by method and URL, until one with the
  // same method and URL is answered.
  let lost = new Map()
  page.on("pageerror", (err) => {
    problems.push(`page error: ${firstLine(err.message)}`)
  })
  page.on("console", (msg) => {
    if (msg.type() !== "error") return
    const text = msg.text()
    // Chrome's "Failed to load resource" text names no URL; the location is
    // the request it is about.
    const where = msg.location().url
    if (NETWORK_CHANGED_NOTE.test(text)) return
    if (STATUS_NOTE.test(text) && where && new URL(where).origin === origin) {
      return
    }
    problems.push(
      `console error: ${firstLine(text)}${where ? ` (${where})` : ""}`
    )
  })
  page.on("response", (res) => {
    const url = new URL(res.url())
    if (url.origin !== origin) return
    const method = res.request().method()
    lost.delete(`${method} ${url.href}`)
    if (res.status() >= 400 && !expected(method, url.pathname, res.status())) {
      problems.push(`${res.status()} from ${method} ${url.pathname}`)
    }
  })
  page.on("requestfailed", (req) => {
    const url = new URL(req.url())
    if (url.origin === origin && req.failure()?.errorText === NETWORK_CHANGED) {
      lost.set(`${req.method()} ${url.href}`, `${req.method()} ${url.pathname}`)
    }
  })
  page.on("crash", () => {
    problems.push("the page crashed")
  })

  const width = Math.max(...routes.map((route) => route.path.length))
  let failed = 0
  for (const route of routes) {
    problems = []
    lost = new Map()
    const started = Date.now()
    let result = ""
    try {
      // A route's seed runs on the page before it, signed in on this origin.
      if (route.before) await route.before(page)
      await page.goto(origin + route.path, { waitUntil: "load" })
      result = await route.check(page, origin)
      await page.waitForTimeout(SETTLE_MS)
    } catch (err) {
      problems.push(firstLine(err.message))
    }
    const deadline = Date.now() + RETRY_MS
    while (lost.size > 0 && Date.now() < deadline) {
      await page.waitForTimeout(250)
    }
    for (const request of lost.values()) {
      problems.push(
        `${request} was lost to ${NETWORK_CHANGED} and no retry answered`
      )
    }
    const seconds = ((Date.now() - started) / 1000).toFixed(1)
    const path = route.path.padEnd(width)
    if (problems.length === 0) {
      console.log(`console:render: ok   ${path}  ${result} (${seconds}s)`)
    } else {
      failed++
      console.log(`console:render: FAIL ${path}  ${problems.join("; ")}`)
    }
  }
  await browser.close()

  if (failed > 0) {
    console.error(`console:render: ${failed} of ${routes.length} routes failed`)
    process.exit(1)
  }
}

const arg = process.argv[2]
try {
  if (arg === "--prepare") {
    await prepare()
  } else if (arg && !arg.startsWith("-")) {
    await render(arg)
  } else {
    die("usage: render-smoke.mjs <base URL> | --prepare")
  }
} catch (err) {
  die(firstLine(err.message))
}
