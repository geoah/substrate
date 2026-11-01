import { describe, expect, it } from "vitest"

import type { AuthorizationKind, SubstrateRecord } from "@/lib/api/types"
import { packageName } from "@/lib/app-setup"

import {
  overAgents,
  scopeSentence,
  scopeWords,
  webhookSentence,
  type SentenceScope,
} from "./app-scopes"

describe("scopeWords", () => {
  it("names what each verb does over its pattern", () => {
    expect(scopeWords("read:ada.example.com/tasks/*")).toEqual({
      verb: "read",
      action: "Read records of",
      pattern: "ada.example.com/tasks/*",
    })
    expect(scopeWords("write:ada.example.com/tasks/task").action).toBe(
      "Read and write records of"
    )
    expect(scopeWords("agents:ada.example.com/llm/*")).toEqual({
      verb: "agents",
      action: "Talk to, and read the chats of, the agents matching",
      pattern: "ada.example.com/llm/*",
    })
    expect(scopeWords("decide:*").pattern).toBe("*")
  })

  it("names the webhook a webhook scope delivers to", () => {
    expect(scopeWords("webhook:applehealth-webhook")).toEqual({
      verb: "webhook",
      action: "Send data to, and see the refused deliveries of, the webhook",
      pattern: "applehealth-webhook",
    })
  })

  it("keeps a scope it does not know as written", () => {
    expect(scopeWords("admin:*")).toEqual({
      verb: undefined,
      action: "",
      pattern: "admin:*",
    })
  })
})

describe("overAgents", () => {
  it("is true for the two agent verbs alone", () => {
    expect(overAgents("agents")).toBe(true)
    expect(overAgents("decide")).toBe(true)
    expect(overAgents("read")).toBe(false)
    expect(overAgents("write")).toBe(false)
    expect(overAgents("webhook")).toBe(false)
  })
})

const APPLE = "providers.substrate.reamde.dev/apple"

function kind(
  identity: string,
  plural?: string,
  purpose?: string
): AuthorizationKind {
  return {
    identity,
    ...(plural ? { label: { singular: plural, plural } } : {}),
    ...(purpose ? { purpose } : {}),
  }
}

function scope(over: Partial<SentenceScope>): SentenceScope {
  return {
    verb: "read",
    pattern: "*",
    glob: false,
    kinds: [],
    agents: [],
    ...over,
  }
}

function agent(id: string): SubstrateRecord {
  return {
    id,
    kind: "substrate.reamde.dev/core/agent",
    properties: {},
    labels: {},
    version: 1,
    createdAt: "2026-10-01T00:00:00Z",
    updatedAt: "2026-10-01T00:00:00Z",
  }
}

describe("scopeSentence", () => {
  it("reads every kind of a package by its labels, the primary ones named and the rest counted", () => {
    expect(
      scopeSentence(
        scope({
          verb: "write",
          pattern: `${APPLE}/*`,
          glob: true,
          kinds: [
            kind(`${APPLE}/addressbook`, "Address books", "supporting"),
            kind(`${APPLE}/contact`, "Contacts"),
            kind(`${APPLE}/event`, "Events"),
            kind(`${APPLE}/reminder`, "Reminders"),
            kind(`${APPLE}/visit`, "Visits"),
          ],
        })
      )
    ).toBe(
      "See, add, change and delete your Apple contacts, events, reminders and visits, and 1 more kind, including kinds added later"
    )
  })

  it("says a read sees, and a write may also add, change and delete", () => {
    const tasks = [kind("ada.example.com/tasks/task", "Tasks")]
    expect(
      scopeSentence(
        scope({ pattern: "ada.example.com/tasks/task", kinds: tasks })
      )
    ).toBe("See your tasks")
    expect(
      scopeSentence(
        scope({
          verb: "write",
          pattern: "ada.example.com/tasks/task",
          kinds: tasks,
        })
      )
    ).toBe("See, add, change and delete your tasks")
  })

  it("names a kind with no label by its own words, and one with nothing installed yet by its pattern", () => {
    expect(
      scopeSentence(
        scope({
          pattern: "ada.example.com/tasks/*",
          glob: true,
          kinds: [
            kind("ada.example.com/tasks/task", "Tasks"),
            kind("ada.example.com/tasks/project"),
          ],
        })
      )
    ).toBe("See your tasks and projects, including kinds added later")
    expect(
      scopeSentence(
        scope({ verb: "write", pattern: "ada.example.com/notes/note" })
      )
    ).toBe("See, add, change and delete your notes")
    expect(
      scopeSentence(scope({ pattern: "ada.example.com/notes/*", glob: true }))
    ).toBe("See your Notes records, including kinds added later")
    expect(
      scopeSentence(scope({ pattern: "substrate.reamde.dev/llm/provider" }))
    ).toBe("See your LLM providers")
  })

  // The server refuses such a scope, so the sentence fixture never carries
  // one; the page still reads it without an empty package name.
  it("reads a pattern that names no package by its kind alone", () => {
    expect(scopeSentence(scope({ pattern: "note" }))).toBe("See your notes")
  })

  it("groups the kinds of several packages", () => {
    expect(
      scopeSentence(
        scope({
          pattern: "ada.example.com/*",
          glob: true,
          kinds: [
            kind("ada.example.com/people/person", "People"),
            kind("ada.example.com/tasks/project", "Projects"),
            kind("ada.example.com/tasks/task", "Tasks"),
          ],
        })
      )
    ).toBe(
      "See your people, and projects and tasks, including kinds added later"
    )
  })

  it("reads every record as every record", () => {
    expect(scopeSentence(scope({ pattern: "*", glob: true }))).toBe(
      "See all your records, including kinds added later"
    )
  })

  it("says talking to agents reaches whatever they may do, reads every chat with them and may rename or stop one", () => {
    expect(
      scopeSentence(scope({ verb: "agents", pattern: "*", glob: true }))
    ).toBe(
      "Talk to all your agents, including agents added later, and through a chat have them do anything they may do; read every chat with them, yours included, answer their questions, and rename or stop any of those chats"
    )
    expect(
      scopeSentence(
        scope({
          verb: "agents",
          pattern: "ada.example.com/llm/substrateEditor",
          agents: [agent("ada.example.com/llm/substrateEditor")],
        })
      )
    ).toBe(
      "Talk to Substrate editor, and through a chat have it do anything it may do; read every chat with it, yours included, answer its questions, and rename or stop any of those chats"
    )
  })

  it("says deciding may edit a suggested change before applying it, as you", () => {
    expect(
      scopeSentence(
        scope({
          verb: "decide",
          pattern: "ada.example.com/llm/substrateEditor",
          agents: [agent("ada.example.com/llm/substrateEditor")],
        })
      )
    ).toBe(
      "Apply or dismiss the changes Substrate editor suggests, and edit one before applying it, as you"
    )
    expect(
      scopeSentence(scope({ verb: "decide", pattern: "*", glob: true }))
    ).toBe(
      "Apply or dismiss the changes your agents suggest, including agents added later, and edit one before applying it, as you"
    )
    expect(
      scopeSentence(
        scope({ verb: "decide", pattern: "b.example.com/x/*", glob: true })
      )
    ).toBe(
      "Apply or dismiss the changes the agents in X suggest, including agents added later, and edit one before applying it, as you"
    )
  })

  it("says a webhook scope sends data and sees every delivery that could not be filed, whoever sent it", () => {
    expect(
      scopeSentence(scope({ verb: "webhook", pattern: "applehealth-webhook" }))
    ).toBe(
      "Send data to your repository through the applehealth-webhook webhook, and see every delivery to it that could not be filed, whoever sent it"
    )
    expect(webhookSentence("applehealth-webhook", "Apple Health")).toBe(
      "Send data to your repository through the Apple Health webhook, for any Apple Health account, and see every delivery to it that could not be filed, whoever sent it"
    )
  })

  it("names at most six kinds and counts the rest", () => {
    const kinds = [
      "alpha",
      "beta",
      "gamma",
      "delta",
      "epsilon",
      "zeta",
      "eta",
    ].map((n) =>
      kind(`ada.example.com/greek/${n}`, n[0].toUpperCase() + n.slice(1) + "s")
    )
    expect(
      scopeSentence(scope({ pattern: "ada.example.com/greek/*", kinds }))
    ).toBe(
      "See your Greek alphas, betas, gammas, deltas, epsilons and zetas, and 1 more kind"
    )
  })
})

/** One case of the sentence fixture: a scope as a token stores it, the
 * kinds it reaches today, for a `webhook:` scope the package its trigger's
 * callable belongs to, and the sentence the approval page shows for it. */
interface SentenceCase {
  scope: string
  kinds: AuthorizationKind[]
  package?: string
  sentence: string
}

const APPLEHEALTH = "providers.substrate.reamde.dev/applehealth"

function labelled(
  identity: string,
  singular: string,
  plural: string,
  purpose?: string
): AuthorizationKind {
  return {
    identity,
    label: { singular, plural },
    ...(purpose ? { purpose } : {}),
  }
}

/** Every verb over every pattern shape: all, one authority's, one
 * package's and one kind or agent, with and without the kinds it reaches. */
const SENTENCE_CASES: Omit<SentenceCase, "sentence">[] = [
  { scope: "read:*", kinds: [] },
  {
    scope: "read:ada.example.com/*",
    kinds: [
      labelled("ada.example.com/people/person", "Person", "People"),
      labelled("ada.example.com/tasks/project", "Project", "Projects"),
      { identity: "ada.example.com/tasks/task" },
    ],
  },
  { scope: "read:ada.example.com/notes/*", kinds: [] },
  {
    scope: `read:${APPLEHEALTH}/account`,
    kinds: [
      labelled(`${APPLEHEALTH}/account`, "Account", "Accounts", "internal"),
    ],
  },
  { scope: "read:substrate.reamde.dev/llm/provider", kinds: [] },
  {
    scope: "read:ada.example.com/greek/*",
    kinds: ["alpha", "beta", "gamma", "delta", "epsilon", "zeta", "eta"].map(
      (n) => {
        const word = n[0].toUpperCase() + n.slice(1)
        return labelled(`ada.example.com/greek/${n}`, word, `${word}s`)
      }
    ),
  },
  { scope: "write:*", kinds: [] },
  {
    scope: `write:${APPLE}/*`,
    kinds: [
      labelled(
        `${APPLE}/addressbook`,
        "Address book",
        "Address books",
        "supporting"
      ),
      labelled(`${APPLE}/contact`, "Contact", "Contacts"),
      labelled(`${APPLE}/event`, "Event", "Events"),
      labelled(`${APPLE}/reminder`, "Reminder", "Reminders"),
      labelled(`${APPLE}/visit`, "Visit", "Visits"),
    ],
  },
  { scope: "write:ada.example.com/notes/note", kinds: [] },
  { scope: "agents:*", kinds: [] },
  { scope: "agents:b.example.com/*", kinds: [] },
  { scope: "agents:ada.example.com/llm/*", kinds: [] },
  { scope: "agents:ada.example.com/llm/substrateEditor", kinds: [] },
  { scope: "decide:*", kinds: [] },
  { scope: "decide:b.example.com/x/*", kinds: [] },
  { scope: "decide:ada.example.com/llm/substrateEditor", kinds: [] },
  { scope: "webhook:applehealth-webhook", kinds: [], package: APPLEHEALTH },
  { scope: "webhook:applehealth-webhook", kinds: [] },
]

/** A case's sentence, from the functions the approval page uses: a glob is
 * a pattern that ends in `*`, and a `webhook:` scope names its package by
 * the display name of the id's last segment. */
function sentenceOf(c: Omit<SentenceCase, "sentence">): string {
  const { verb, pattern } = scopeWords(c.scope)
  if (!verb) throw new Error(`${c.scope} names no verb`)
  if (verb === "webhook")
    return webhookSentence(
      pattern,
      c.package ? packageName(c.package) : undefined
    )
  return scopeSentence({
    verb,
    pattern,
    glob: pattern === "*" || pattern.endsWith("/*"),
    kinds: c.kinds,
    agents: [],
  })
}

describe("the sentence fixture", () => {
  // SubstrateKit's tests read the same file, so a scope reads the same on
  // the approval page and in an app. `vitest run -u` writes it again.
  it("is what the approval page says for every verb and pattern shape", async () => {
    const cases: SentenceCase[] = SENTENCE_CASES.map((c) => ({
      scope: c.scope,
      kinds: c.kinds,
      ...(c.package ? { package: c.package } : {}),
      sentence: sentenceOf(c),
    }))
    await expect(`${JSON.stringify(cases, null, 2)}\n`).toMatchFileSnapshot(
      "./app-scopes.fixture.json"
    )
  })

  // The server refuses a kind pattern that names no package, so the file the
  // kit reads never carries one.
  it("carries only kind patterns the server accepts", () => {
    for (const c of SENTENCE_CASES) {
      const { verb, pattern } = scopeWords(c.scope)
      if ((verb === "read" || verb === "write") && pattern !== "*")
        expect(pattern, c.scope).toContain("/")
    }
  })
})
