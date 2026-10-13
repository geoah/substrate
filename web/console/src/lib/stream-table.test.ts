/** The streams table in the records lists' terms: the fields its filter
 * bar offers, the rows a filter and a search keep, the sort and the
 * grouping, the selection the address keeps (one row, a shift-run, every
 * row shown), the reads a selection makes per metric, and a saved view's
 * shape, which never holds the search or the selection. */

import { describe, expect, it } from "vitest"

import { encodeFilter } from "@/lib/filters"
import { streamsOf, type Stream } from "@/lib/observations"
import {
  isAllView,
  viewFromState,
  viewMatches,
  viewsFrom,
} from "@/lib/saved-views"
import {
  APPLE,
  BODY_MASS,
  GARMIN,
  HEART,
  LATEST,
  METRIC,
  MIA,
  SLEEP_STAGE,
  SOURCE,
  WHOOP,
  WITHINGS,
} from "@/lib/observations.test-fixtures"
import {
  DEFAULT_STREAM_SORT,
  OWNER,
  STREAM_VIEWS,
  choiceFilter,
  groupStreams,
  parseStreamSort,
  selectRange,
  selectedStreams,
  selectionByMetric,
  selectionState,
  selectionToken,
  shortRef,
  sortStreams,
  streamChoices,
  streamFields,
  streamMatches,
  streamViewState,
  tokenMetric,
  tokenNames,
  type StreamRow,
} from "./stream-table"

const PINS = { metric: METRIC, source: SOURCE }

const NAMES: Record<string, string> = {
  [HEART]: "Heart rate",
  [BODY_MASS]: "Weight",
  [SLEEP_STAGE]: "Sleep stage",
}

function rowsOf(streams: Stream[] = streamsOf(LATEST)): StreamRow[] {
  return streams.map((s) => ({
    ...s,
    metricName:
      NAMES[s.metric] ?? s.metric.slice(s.metric.lastIndexOf("/") + 1),
    sourceTitle: s.source?.slice(s.source.lastIndexOf("/") + 1),
    shape: s.code ? "coded" : ((s.form ?? "gauge") as StreamRow["shape"]),
  }))
}

const NONE = streamChoices([])

describe("the filter bar's fields", () => {
  it("offers each stream column as a set of values to pick, words beside each", () => {
    const fields = streamFields({
      metric: [{ value: HEART, label: "Heart rate" }],
      source: [
        { value: GARMIN, label: "Garmin Forerunner 265" },
        { value: APPLE, label: "Apple Watch S10" },
      ],
      about: [],
    })
    expect(fields.map((f) => f.name)).toEqual(["metric", "source"])
    expect(fields[1]).toMatchObject({
      kind: "enum",
      repeated: false,
      values: [
        { value: GARMIN, label: "Garmin Forerunner 265" },
        { value: APPLE, label: "Apple Watch S10" },
      ],
    })
  })

  it("reads the chosen values from the address's filter tokens", () => {
    const tokens = [
      encodeFilter(choiceFilter("source", [GARMIN, APPLE])),
      encodeFilter(choiceFilter("shape", ["gauge"])),
      encodeFilter({ field: "nonsense", op: "eq", value: "x" }),
    ]
    expect(streamChoices(tokens)).toEqual({
      metric: [],
      source: [GARMIN, APPLE],
      about: [],
      shape: ["gauge"],
      channel: [],
    })
  })

  it("keeps a channel holding a comma as one value", () => {
    const fields = streamFields({
      channel: [
        { value: "run,easy", label: "run,easy" },
        { value: "ride", label: "ride" },
      ],
    })
    const filter = choiceFilter("channel", ["run,easy", "ride"])
    // The bar picks by the values it splits the filter's into.
    const offered = fields[0].values!.map((v) => v.value)
    for (const part of filter.value.split(",")) {
      expect(offered).toContain(part)
    }
    expect(streamChoices([encodeFilter(filter)]).channel).toEqual([
      "run,easy",
      "ride",
    ])
  })
})

describe("the rows a filter keeps", () => {
  const rows = rowsOf()

  it("keeps any of the chosen values, by path or by id", () => {
    const kept = rows.filter((r) =>
      streamMatches(r, { ...NONE, source: ["withings-body-plus", GARMIN] }, "")
    )
    expect(kept.map((r) => r.source)).toEqual([GARMIN, GARMIN, WITHINGS])
  })

  it("reads You as the streams about nobody", () => {
    const mine = rows.filter((r) =>
      streamMatches(r, { ...NONE, about: [OWNER] }, "")
    )
    expect(mine).toHaveLength(5)
    expect(
      rows.filter((r) => streamMatches(r, { ...NONE, about: [MIA] }, ""))
    ).toHaveLength(1)
  })

  it("keeps a channel's streams apart", () => {
    const whoop = rowsOf([
      { ...rows[0], key: "a", source: WHOOP, channel: "cycle" },
      { ...rows[0], key: "b", source: WHOOP, channel: "workout" },
    ])
    expect(
      whoop
        .filter((r) => streamMatches(r, { ...NONE, channel: ["workout"] }, ""))
        .map((r) => r.key)
    ).toEqual(["b"])
  })

  it("matches every word of the search in any of the row's words", () => {
    expect(
      rows.filter((r) => streamMatches(r, NONE, "heart garmin")).length
    ).toBe(1)
    expect(rows.filter((r) => streamMatches(r, NONE, "level")).length).toBe(3)
  })
})

describe("sorting and grouping", () => {
  const rows = rowsOf()

  it("sorts by a column either way, the rows with nothing there last", () => {
    expect(parseStreamSort("count:asc")).toEqual({ id: "count", desc: false })
    expect(parseStreamSort("nonsense:asc")).toEqual({ id: "when", desc: true })
    expect(sortStreams(rows, "count:asc").map((r) => r.count)).toEqual([
      1, 4, 9, 96, 310, 1812,
    ])
    const latest = sortStreams(rows, "latest:desc").map((r) => r.value)
    // A code is no number: it sorts as words after them.
    expect(latest.slice(0, 3)).toEqual(["412", "96", "72.40"])
  })

  it("keeps the order a stream came in where the column ties", () => {
    const sorted = sortStreams(rows, "shape:asc")
    const levels = sorted.filter((r) => r.shape === "gauge").map((r) => r.key)
    expect(levels).toEqual(
      rows.filter((r) => r.shape === "gauge").map((r) => r.key)
    )
  })

  it("groups the rows in runs, in the order of their words", () => {
    const grouped = groupStreams(
      sortStreams(rows, DEFAULT_STREAM_SORT),
      "metric",
      (key) => rows.find((r) => r.metric === key)!.metricName
    )
    const runs = grouped.map((r) => r.metric)
    expect(runs.indexOf(HEART)).toBeLessThan(runs.indexOf(BODY_MASS))
    // Heart rate's two streams sit together, newest first.
    expect(runs.slice(0, 2)).toEqual([HEART, HEART])
    expect(grouped[0].source).toBe(GARMIN)
  })

  it("puts You first when grouped by what a stream is about", () => {
    const grouped = groupStreams(rows, "about", (key) => key)
    expect(grouped[0].about).toBeUndefined()
    expect(grouped.at(-1)!.about).toBe(MIA)
  })
})

describe("the selection in the address", () => {
  const rows = rowsOf()

  it("names a stream by its metric, source, thing and channel, by id where it can", () => {
    const token = selectionToken(rows[0], PINS)
    expect(token).toBe("health/heart_rate|garmin-forerunner-265||")
    expect(tokenMetric(token)).toBe("health/heart_rate")
    expect(tokenNames(token, rows[0])).toBe(true)
    expect(tokenNames(token, rows[2])).toBe(false)
    expect(
      selectionToken({ ...rows[0], channel: "a|b,c" }, PINS).split("|")
    ).toHaveLength(4)
    expect(
      tokenNames(selectionToken({ ...rows[0], channel: "a|b,c" }, PINS), {
        ...rows[0],
        channel: "a|b,c",
      })
    ).toBe(true)
  })

  it("keeps a record path an id could be mistaken for", () => {
    expect(shortRef(`${SOURCE}/garmin-forerunner-265`, SOURCE)).toBe(
      "garmin-forerunner-265"
    )
    // An id whose first segment carries a dot reads as a path: kept whole.
    expect(shortRef(`${SOURCE}/a.b/c/d/e`, SOURCE)).toBe(`${SOURCE}/a.b/c/d/e`)
    expect(shortRef(MIA, SOURCE)).toBe(MIA)
  })

  it("reads the selected streams in the order they were selected", () => {
    const tokens = [
      selectionToken(rows[3], PINS),
      selectionToken(rows[0], PINS),
    ]
    expect(selectedStreams(tokens, rows)).toEqual([rows[3], rows[0]])
    // A token naming no stream is skipped.
    expect(selectedStreams(["gone|x||"], rows)).toEqual([])
  })

  it("checks one row, then a shift-run from it, then clears a run", () => {
    let tokens = selectRange([], rows, 1, 1, true, PINS)
    expect(selectedStreams(tokens, rows)).toEqual([rows[1]])
    tokens = selectRange(tokens, rows, 1, 3, true, PINS)
    expect(selectedStreams(tokens, rows)).toEqual([rows[1], rows[2], rows[3]])
    // Backwards is the same run.
    tokens = selectRange(tokens, rows, 3, 2, false, PINS)
    expect(selectedStreams(tokens, rows)).toEqual([rows[1]])
  })

  it("says whether every row shown, some or none is selected", () => {
    const one = selectRange([], rows, 0, 0, true, PINS)
    expect(selectionState([], rows)).toBe("none")
    expect(selectionState(one, rows)).toBe("some")
    expect(selectionState(one, rows.slice(0, 1))).toBe("all")
    const every = selectRange(one, rows, 0, rows.length - 1, true, PINS)
    expect(every).toHaveLength(rows.length)
    expect(every[0]).toBe(one[0])
    expect(selectionState(every, rows)).toBe("all")
  })

  it("reads each metric once, narrowed to its selected sources", () => {
    const [garminHeart, , appleHeart] = [rows[0], rows[1], rows[2]]
    expect(appleHeart.metric).toBe(HEART)
    const sleep = rows.find((r) => r.metric === SLEEP_STAGE)!
    const weight = rows.find((r) => r.metric === BODY_MASS)!
    const byMetric = selectionByMetric([appleHeart, weight, garminHeart, sleep])
    expect(byMetric.map((e) => e.metric)).toEqual([
      HEART,
      BODY_MASS,
      SLEEP_STAGE,
    ])
    expect(byMetric[0]).toMatchObject({
      sources: [APPLE, GARMIN],
      // Both are the owner's: no thing to narrow by.
      abouts: [],
      channels: [],
    })
    // Newest first, as a plot reads its forms.
    expect(byMetric[0].streams[0]).toBe(garminHeart)
    // Every selected sleep stream is about Mia.
    expect(byMetric[2].abouts).toEqual([MIA])
  })
})

describe("a saved view of the table", () => {
  const page = {
    filter: [encodeFilter(choiceFilter("source", [GARMIN]))],
    sort: "count:asc",
    group: "metric" as const,
    columns: [
      "select",
      "metric",
      "source",
      "shape",
      "latest",
      "when",
      "count",
      "coverage",
    ],
    hidden: ["coverage"],
  }

  it("names the filters, the sort, the grouping and the columns, and matches again", () => {
    const state = streamViewState(page)
    const view = viewFromState(
      { id: "v1", collection: STREAM_VIEWS, name: "Garmin" },
      state,
      DEFAULT_STREAM_SORT
    )
    expect(view).toEqual({
      id: "v1",
      collection: "observations/streams",
      name: "Garmin",
      filter: page.filter,
      sort: "count:asc",
      group: "metric",
      columns: page.columns,
      hidden: ["coverage"],
    })
    // Through the preference record and back, it still names the page.
    const [stored] = viewsFrom(JSON.parse(JSON.stringify([view])))
    expect(viewMatches(stored, state, DEFAULT_STREAM_SORT)).toBe(true)
    expect(
      viewMatches(
        stored,
        streamViewState({ ...page, sort: DEFAULT_STREAM_SORT }),
        DEFAULT_STREAM_SORT
      )
    ).toBe(false)
  })

  it("never holds the search or the selection", () => {
    const state = streamViewState({
      ...page,
      ...({ search: "heart", select: ["health/heart_rate|x||"] } as object),
    })
    expect(Object.keys(state).sort()).toEqual([
      "columns",
      "filter",
      "group",
      "hidden",
      "nest",
      "sort",
    ])
  })

  it("is All while nothing narrows, sorts or groups the table", () => {
    expect(
      isAllView(
        streamViewState({
          filter: [],
          sort: DEFAULT_STREAM_SORT,
          columns: [],
          hidden: [],
        }),
        DEFAULT_STREAM_SORT
      )
    ).toBe(true)
  })
})
