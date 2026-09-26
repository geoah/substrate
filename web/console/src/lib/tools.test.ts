import { describe, expect, it } from "vitest"

import type { SubstrateRecord } from "@/lib/api/types"
import {
  argumentLabel,
  buildCallInput,
  buildTools,
  cadenceSummary,
  cadenceWords,
  canTryIt,
  effectsWords,
  groupTools,
  isPaused,
  isoDurationWords,
  kindLabeler,
  nameWords,
  outputRows,
  permissionWords,
  permissionsYaml,
  refId,
  runFromToolMessage,
  runFromTriggerRun,
  toolDescription,
  toolIconName,
  toolMessageRuns,
  toolName,
  toolStarts,
  toolStatus,
  usageNames,
  tookWords,
  type Tool,
} from "./tools"

const KIND = "substrate.reamde.dev/core/kind"

function rec(
  kind: string,
  id: string,
  properties: Record<string, unknown>,
  over: Partial<SubstrateRecord> = {}
): SubstrateRecord {
  return {
    id,
    kind,
    properties,
    labels: {},
    version: 1,
    createdAt: "2026-09-25T10:00:00Z",
    updatedAt: "2026-09-25T10:00:00Z",
    ...over,
  }
}

const fn = (id: string, properties: Record<string, unknown> = {}) =>
  rec("substrate.reamde.dev/core/function", id, {
    runtime: "python",
    description: "Does a thing. And more.",
    ...properties,
  })

const agent = (id: string, tools: unknown[]) =>
  rec("substrate.reamde.dev/core/agent", id, { tools })

const trigger = (
  id: string,
  callable: string,
  source: Record<string, unknown>,
  enabled = true
) =>
  rec("substrate.reamde.dev/core/trigger", id, {
    callable: { ref: `substrate.reamde.dev/core/function/${callable}` },
    enabled,
    source,
  })

const GCAL = "providers.substrate.reamde.dev/google/synccalendar"
const SAVE = "ada.example.com/notes/savenote"
const STATS = "ada.example.com/notes/stats"
const QUERY = "substrate.reamde.dev/core/query"
const WRITE = "substrate.reamde.dev/core/write"
const IDLE = "ada.example.com/notes/archive"

function fixture(): Tool[] {
  return buildTools(
    [
      fn(GCAL, {
        permissions: {
          reads: {
            kinds: [
              { ref: `${KIND}/providers.substrate.reamde.dev/google/calendar` },
            ],
          },
          writes: [
            { ref: `${KIND}/providers.substrate.reamde.dev/google/calendar` },
            {
              ref: `${KIND}/providers.substrate.reamde.dev/google/calendarevent`,
            },
          ],
          network: ["www.googleapis.com"],
        },
      }),
      fn(SAVE, {
        permissions: {
          writes: [{ ref: `${KIND}/ada.example.com/notes/note` }],
        },
        arguments: [
          { name: "id", type: "string", required: true },
          { name: "text", type: "string", required: true },
          { name: "words", type: "int" },
        ],
        returns: [{ name: "saved", type: "string" }],
      }),
      fn(STATS),
      fn(QUERY, { runtime: "host" }),
      fn(WRITE, { runtime: "host" }),
      fn(IDLE),
    ],
    [
      agent("ada.example.com/notes/notekeeper", [
        { function: { ref: `substrate.reamde.dev/core/function/${SAVE}` } },
        {
          function: { ref: `substrate.reamde.dev/core/function/${STATS}` },
          name: "count",
        },
      ]),
      agent("ada.example.com/llm/substrate", [
        { function: { ref: `substrate.reamde.dev/core/function/${QUERY}` } },
      ]),
    ],
    [
      trigger("google-calendar-scheduled", GCAL, {
        schedule: { recurrence: "FREQ=HOURLY", timezone: "Europe/London" },
      }),
      trigger("google-calendar-on-request", GCAL, {
        record: {
          kinds: ["providers.substrate.reamde.dev/google/account"],
          ops: ["create", "update"],
          when: 'record.properties.syncRequestedAt != ""',
        },
      }),
      trigger("google-calendar-on-connect", GCAL, {
        record: {
          kinds: ["providers.substrate.reamde.dev/google/account"],
          ops: ["create", "update"],
          when: "true",
        },
      }),
    ],
    "ada.example.com"
  )
}

const byRef = (tools: Tool[], ref: string) => {
  const t = tools.find((x) => x.ref === ref)
  if (!t) throw new Error(`no ${ref}`)
  return t
}

const label = kindLabeler()

describe("refId", () => {
  it("drops the kind's three segments, keeping an id with slashes", () => {
    expect(refId({ ref: `${KIND}/a.example.com/p/n` })).toBe(
      "a.example.com/p/n"
    )
    expect(refId("substrate.reamde.dev/core/trigger/t1")).toBe("t1")
    expect(refId({})).toBeUndefined()
    expect(refId("too/short")).toBeUndefined()
  })
})

describe("buildTools", () => {
  it("joins each function with the agents that list it and its triggers", () => {
    const tools = fixture()
    expect(byRef(tools, SAVE).uses).toEqual([
      { agent: "ada.example.com/notes/notekeeper", name: "savenote" },
    ])
    expect(byRef(tools, STATS).uses[0].name).toBe("count")
    expect(byRef(tools, GCAL).triggers.map((t) => t.id)).toEqual([
      "google-calendar-on-connect",
      "google-calendar-on-request",
      "google-calendar-scheduled",
    ])
    expect(byRef(tools, SAVE).origin).toEqual({ kind: "yours" })
    expect(byRef(tools, QUERY).origin).toEqual({ kind: "core" })
    expect(byRef(tools, GCAL).origin.kind).toBe("provider")
  })
})

describe("names", () => {
  it.each([
    [QUERY, "Look things up"],
    ["substrate.reamde.dev/core/propose", "Suggest a change"],
    [WRITE, "Make a change"],
    ["substrate.reamde.dev/core/ask", "Ask you a question"],
    [SAVE, "Save note"],
    [STATS, "Stats"],
    ["a.example.com/readinglist/fetchpage", "Fetch page"],
    ["a.example.com/readinglist/setclass", "Set class"],
    ["a.example.com/x/noteTitle", "Note title"],
    [GCAL, "Google Calendar sync"],
  ])("%s reads %s", (ref, name) => {
    expect(toolName(ref)).toBe(name)
  })

  it("splits a local name into words", () => {
    expect(nameWords("savenote")).toEqual(["save", "note"])
    expect(nameWords("zzz")).toEqual(["zzz"])
  })

  it("picks an icon from what the name says", () => {
    expect(toolIconName(QUERY)).toBe("search")
    expect(toolIconName(WRITE)).toBe("pencil")
    expect(toolIconName("substrate.reamde.dev/core/ask")).toBe(
      "message-circle-question"
    )
    expect(toolIconName(GCAL)).toBe("calendar")
    expect(toolIconName("p.example.com/google/synccontacts")).toBe("at-sign")
    expect(toolIconName(SAVE)).toBe("file-text")
    expect(toolIconName(STATS)).toBe("hash")
    expect(toolIconName(IDLE)).toBe("wrench")
  })

  it("reads the first clause, and core's four in everyday words", () => {
    expect(
      toolDescription({
        ref: "providers.substrate.reamde.dev/google/synccalendar",
        description:
          "Sync a Google account's calendars: drain each calendar's events on its own sync token.",
      })
    ).toBe("Sync a Google account's calendars.")
    expect(
      toolDescription({ ref: QUERY, description: "Read records." })
    ).toMatch(/^Finds and reads records/)
  })
})

describe("groupTools", () => {
  it("files agents' tools, syncs and the rest, core last in everyday mode", () => {
    const groups = groupTools(fixture(), false)
    expect(groups.map((g) => [g.key, g.tools.map((t) => t.ref)])).toEqual([
      ["agents", [SAVE, STATS]],
      ["syncs", [GCAL]],
      ["other", [IDLE]],
      ["builtin", [QUERY, WRITE]],
    ])
  })

  it("files core with everything else in technical mode", () => {
    const groups = groupTools(fixture(), true)
    expect(groups.map((g) => g.key)).toEqual(["agents", "syncs", "other"])
    expect(groups[0].tools.map((t) => t.ref)).toContain(QUERY)
    expect(groups[2].tools.map((t) => t.ref)).toEqual([IDLE, WRITE])
  })
})

describe("when it runs", () => {
  it.each([
    ["FREQ=HOURLY", "Every hour"],
    ["FREQ=MINUTELY;INTERVAL=15", "Every 15 minutes"],
    ["FREQ=HOURLY;INTERVAL=6", "Every 6 hours"],
    ["FREQ=DAILY;BYHOUR=9", "Every day at 09:00"],
    ["FREQ=WEEKLY;BYDAY=MO,FR", "Every Monday and Friday"],
    ["RRULE:FREQ=MONTHLY", "Every month"],
    ["garbage", "On a schedule"],
  ])("%s → %s", (rule, words) => {
    expect(cadenceWords(rule)).toBe(words)
  })

  it("lists a sync's schedule, its connect run and Sync now", () => {
    const starts = toolStarts(byRef(fixture(), GCAL), label)
    expect(starts.map((s) => [s.label, s.detail])).toEqual([
      ["On a change", "When an account is first connected"],
      ["By you", "When you press Sync now"],
      ["On a schedule", "Every hour"],
    ])
    expect(cadenceSummary(byRef(fixture(), GCAL), label)).toBe("Every hour")
  })

  it("says an agent tool runs by an agent and by you when it can be tried", () => {
    const starts = toolStarts(byRef(fixture(), SAVE), label)
    expect(starts.map((s) => s.label)).toEqual(["By an agent", "By you"])
    expect(cadenceSummary(byRef(fixture(), IDLE), label)).toBe(
      "Nothing runs it yet"
    )
  })

  it("words a generic change trigger by the kinds it watches", () => {
    const [t] = buildTools(
      [fn("a.example.com/p/f")],
      [],
      [
        trigger("t", "a.example.com/p/f", {
          record: { kinds: ["a.example.com/tasks/task"], ops: ["create"] },
        }),
      ]
    )
    expect(toolStarts(t, label)[0].detail).toBe("When tasks are added")
  })
})

describe("canTryIt", () => {
  it("offers python tools and query, never the writes or a sync", () => {
    const tools = fixture()
    expect(canTryIt(byRef(tools, SAVE))).toBe(true)
    expect(canTryIt(byRef(tools, QUERY))).toBe(true)
    expect(canTryIt(byRef(tools, WRITE))).toBe(false)
    expect(canTryIt(byRef(tools, GCAL))).toBe(false)
  })
})

describe("permissionWords", () => {
  it("reads kinds as plurals, the internet as hosts", () => {
    const words = permissionWords(byRef(fixture(), GCAL), label)
    expect(words).toEqual({
      sees: "Calendars",
      changes: "Calendars and calendar events",
      internet: "Only www.googleapis.com",
      asks: null,
    })
  })

  it("says nothing where nothing is granted", () => {
    expect(permissionWords(byRef(fixture(), STATS), label)).toEqual({
      sees: null,
      changes: null,
      internet: null,
      asks: null,
    })
  })

  it("describes core's host functions by the calling agent's grants", () => {
    expect(permissionWords(byRef(fixture(), QUERY), label).sees).toBe(
      "Whatever the agent using it may see"
    )
  })

  it("names globs, mutations, calls and caps long lists", () => {
    const [t] = buildTools(
      [
        fn("a.example.com/p/f", {
          permissions: {
            reads: {
              kinds: [
                { ref: `${KIND}/*` },
                { ref: `${KIND}/providers.substrate.reamde.dev/linear/*` },
              ],
            },
            writes: ["a", "b", "c", "d", "e", "f"].map((n) => ({
              ref: `${KIND}/a.example.com/p/${n}`,
            })),
            mutations: ["merge"],
            call: [{ ref: `substrate.reamde.dev/core/function/${SAVE}` }],
          },
        }),
      ],
      [],
      []
    )
    const w = permissionWords(t, label)
    expect(w.sees).toBe("Everything and everything from Linear")
    expect(w.changes).toBe("As, bs, cs, ds and 2 more; can merge records")
    expect(w.asks).toBe("Save note")
    expect(permissionsYaml(t)).toContain("  mutations:\n    - merge")
  })
})

describe("the call form", () => {
  const args = buildTools(
    [
      fn("a.example.com/p/f", {
        arguments: [
          { name: "text", type: "string", required: true },
          { name: "first", type: "int" },
          { name: "filter", type: "json" },
          { name: "tags", type: "string", repeated: true },
          { name: "on", type: "bool" },
        ],
      }),
    ],
    [],
    []
  )[0].arguments

  it("builds the input, leaving empty fields out", () => {
    expect(
      buildCallInput(args, {
        text: " hi ",
        first: "3",
        filter: '{"a":1}',
        tags: "x\n\ny",
        on: true,
      })
    ).toEqual({
      input: {
        text: "hi",
        first: 3,
        filter: { a: 1 },
        tags: ["x", "y"],
        on: true,
      },
      errors: {},
    })
    expect(buildCallInput(args, { text: "hi" }).input).toEqual({ text: "hi" })
  })

  it("names what is missing or malformed", () => {
    expect(buildCallInput(args, { first: "1.5", filter: "{" }).errors).toEqual({
      text: "Needed",
      first: "A whole number",
      filter: "Not valid JSON",
    })
  })

  it("labels arguments and results in words", () => {
    expect(argumentLabel("noteTitle")).toBe("Note title")
    expect(argumentLabel("id")).toBe("ID")
    expect(argumentLabel("parentId")).toBe("Parent ID")
    expect(outputRows({ words: 3, records: [1, 2], ok: true })).toEqual([
      { label: "Words", value: "3" },
      { label: "Records", value: "2 items" },
      { label: "Ok", value: "Yes" },
    ])
    expect(outputRows(null)).toEqual([])
  })
})

describe("runs and status", () => {
  const run = (props: Record<string, unknown>) =>
    rec("substrate.reamde.dev/core/triggerrun", "r1", {
      trigger: {
        ref: "substrate.reamde.dev/core/trigger/google-calendar-scheduled",
      },
      mode: "schedule",
      status: "ok",
      startedAt: "2026-09-25T10:00:00Z",
      finishedAt: "2026-09-25T10:00:03.1Z",
      ...props,
    })

  it("reads a trigger run in words", () => {
    const r = runFromTriggerRun(run({ effects: { put: 12, patch: 2 } }))
    expect(r).toMatchObject({
      by: "schedule",
      trigger: "google-calendar-scheduled",
      happened: "12 saved, 2 changed",
      tookMs: 3100,
      status: "ok",
    })
    expect(runFromTriggerRun(run({})).happened).toBe("Nothing changed")
    expect(
      runFromTriggerRun(run({ status: "skipped", mode: "record" }))
    ).toMatchObject({
      by: "change",
      happened: "Nothing to do",
      status: "skipped",
    })
    expect(
      runFromTriggerRun(run({ status: "parked", reason: "boom\ntrace" }))
    ).toMatchObject({ status: "trouble", happened: "Stopped: boom" })
  })

  it("matches an agent's tool messages back to the tool by name and agent", () => {
    const tools = fixture()
    const msg = (name: string, ok = true) =>
      rec("substrate.reamde.dev/llm/message", `m-${name}`, {
        role: "tool",
        name,
        ok,
        changes: [{ seq: 1 }],
      })
    const runs = toolMessageRuns(
      byRef(tools, STATS),
      [msg("count"), msg("stats"), msg("savenote")],
      () => "ada.example.com/notes/notekeeper"
    )
    expect(runs.map((r) => r.key)).toEqual(["msg:m-count"])
    expect(runs[0].happened).toBe("Changed 1 record")
    expect(
      runFromToolMessage(
        rec("x/y/z", "m", { role: "tool", ok: false, content: "nope" }),
        "a"
      )
    ).toMatchObject({ status: "trouble", happened: "Failed: nope" })
  })

  it("picks the pill: paused, trouble, ran, waiting, never", () => {
    const tools = fixture()
    const gcal = byRef(tools, GCAL)
    const now = Date.parse("2026-09-25T10:05:10Z")
    const ok = runFromTriggerRun(run({}))
    expect(toolStatus(gcal, ok, { now })).toEqual({
      tone: "ok",
      label: "Ran 5 min ago",
    })
    expect(toolStatus(gcal, { ...ok, status: "trouble" }, { now })?.label).toBe(
      "Had trouble"
    )
    expect(
      toolStatus(gcal, undefined, {
        waitingFor: { key: "google", name: "Google", letter: "G", color: "" },
      })
    ).toEqual({ tone: "warn", label: "Waiting for Google" })
    expect(toolStatus(gcal, undefined)?.label).toBe("Never ran")
    const paused = {
      ...gcal,
      triggers: gcal.triggers.map((t) => ({ ...t, enabled: false })),
    }
    expect(isPaused(paused)).toBe(true)
    expect(toolStatus(paused, ok)?.label).toBe("Paused")
  })

  it("carries no pill where nothing records a tool's runs", () => {
    const tools = fixture()
    // No trigger calls it and no agent lists it: a direct call leaves no run.
    expect(toolStatus(byRef(tools, IDLE), undefined)).toBeUndefined()
    expect(toolStatus(byRef(tools, WRITE), undefined)).toBeUndefined()
    // An agent's tool: silent until its calls are counted.
    expect(toolStatus(byRef(tools, QUERY), undefined)).toBeUndefined()
  })

  it("counts an agent's tool by its calls", () => {
    const tools = fixture()
    const query = byRef(tools, QUERY)
    const now = Date.parse("2026-09-25T12:00:00Z")
    const latest = runFromToolMessage(
      rec("substrate.reamde.dev/llm/message", "m1", { role: "tool", ok: true }),
      "ada.example.com/llm/substrate"
    )
    expect(
      toolStatus(query, latest, { usage: { count: 40, latest }, now })
    ).toEqual({ tone: "ok", label: "Used 40 times · last 2 hours ago" })
    expect(
      toolStatus(query, latest, { usage: { count: 1, latest }, now })?.label
    ).toBe("Used once · 2 hours ago")
    expect(toolStatus(query, undefined, { usage: { count: 0 } })).toEqual({
      tone: "neutral",
      label: "Not used yet",
    })
    expect(
      toolStatus(
        query,
        { ...latest, status: "trouble" },
        {
          usage: { count: 3, latest },
        }
      )?.label
    ).toBe("Had trouble")
  })

  it("counts calls by name only where no other tool answers to it", () => {
    const tools = fixture()
    expect(usageNames(byRef(tools, QUERY), tools)).toEqual(["query"])
    expect(usageNames(byRef(tools, STATS), tools)).toEqual(["count"])
    expect(usageNames(byRef(tools, IDLE), tools)).toBeUndefined()
    const clash = buildTools(
      [fn(QUERY, { runtime: "host" }), fn(STATS)],
      [
        agent("a/b/one", [
          { function: { ref: `substrate.reamde.dev/core/function/${QUERY}` } },
        ]),
        agent("a/b/two", [
          {
            function: { ref: `substrate.reamde.dev/core/function/${STATS}` },
            name: "query",
          },
        ]),
      ],
      []
    )
    expect(usageNames(byRef(clash, QUERY), clash)).toBeUndefined()
  })

  it("says durations and effects in words", () => {
    expect(tookWords(420)).toBe("0.4s")
    expect(tookWords(40)).toBe("under 0.1s")
    expect(tookWords(12_400)).toBe("12s")
    expect(tookWords(125_000)).toBe("2 min")
    expect(tookWords(undefined)).toBe("—")
    expect(isoDurationWords("PT5S")).toBe("5 seconds")
    expect(isoDurationWords("PT1M")).toBe("1 minute")
    expect(effectsWords({ delete: 1, merge: 2 })).toBe("2 merged, 1 removed")
  })
})
