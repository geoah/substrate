import { describe, expect, it } from "vitest"

import type { AuthorizationKind, SubstrateRecord } from "@/lib/api/types"
import { packageName } from "@/lib/app-setup"

import {
  ASSISTANT_ACCESS_SENTENCE,
  ASSISTANT_CAN,
  ASSISTANT_CANNOT,
  ASSISTANT_CODE_SENTENCE,
  ASSISTANT_HISTORY_SENTENCE,
  FULL_ACCESS_SENTENCE,
  overAgents,
  scopeSentence,
  scopeTrait,
  scopeWords,
  webhookSentence,
  type SentenceScope,
} from "./app-scopes"

describe("scopeWords", () => {
  it("names what each verb does over its pattern", () => {
    expect(scopeWords("read:~/tasks/*")).toEqual({
      verb: "read",
      action: "Read records of",
      pattern: "~/tasks/*",
    })
    expect(scopeWords("write:~/tasks/task").action).toBe(
      "Read and write records of"
    )
    expect(scopeWords("agents:~/llm/*")).toEqual({
      verb: "agents",
      action: "Talk to, and read the chats of, the agents matching",
      pattern: "~/llm/*",
    })
    expect(scopeWords("decide:*").pattern).toBe("*")
  })

  it("names the functions a call scope runs", () => {
    expect(scopeWords("call:~/tasks/*")).toEqual({
      verb: "call",
      action: "Run the functions matching",
      pattern: "~/tasks/*",
    })
  })

  it("names the webhook a webhook scope delivers to", () => {
    expect(scopeWords("webhook:applehealth-webhook")).toEqual({
      verb: "webhook",
      action: "Send data to, and see the refused deliveries of, the webhook",
      pattern: "applehealth-webhook",
    })
  })

  it("names full access, which has no pattern, by everything it does", () => {
    expect(scopeWords("full")).toEqual({
      verb: "full",
      action: "Everything you can do when you sign in",
      pattern: "",
    })
    expect(
      scopeSentence({
        verb: "full",
        pattern: "",
        glob: false,
        kinds: [],
        agents: [],
        functions: [],
      })
    ).toBe(FULL_ACCESS_SENTENCE)
    expect(FULL_ACCESS_SENTENCE).toMatch(/make or revoke tokens/)
  })

  it("names an AI assistant's access, which has no pattern, by where it works and what it leaves out", () => {
    expect(scopeWords("mcp")).toEqual({
      verb: "mcp",
      action:
        "Work with your data as an AI assistant, at this server’s assistant address alone",
      pattern: "",
    })
    expect(
      scopeSentence({
        verb: "mcp",
        pattern: "",
        glob: false,
        kinds: [],
        agents: [],
        functions: [],
      })
    ).toBe(ASSISTANT_ACCESS_SENTENCE)
    expect(ASSISTANT_ACCESS_SENTENCE).toMatch(/nowhere else/)
    expect(ASSISTANT_ACCESS_SENTENCE).toMatch(/sign-in material/)
  })

  it("says what an assistant can and cannot do, and what its code reaches", () => {
    expect(ASSISTANT_CAN).toEqual([
      "See everything you can, token and credential records included; a secret’s value reads back hidden",
      "Change anything you can, including collections, agents, triggers and packages",
      "Run your tools, agents and syncs",
    ])
    expect(ASSISTANT_CANNOT).toEqual([
      "Write, make or revoke tokens or other sign-in material",
      "See a webhook’s key",
      "Export your data",
      "Connect a provider account",
      "Uninstall or purge a package",
    ])
    expect(ASSISTANT_CODE_SENTENCE).toMatch(
      /secrets included, and hand them back/
    )
    expect(ASSISTANT_HISTORY_SENTENCE).toMatch(/under its own name, not yours/)
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
    expect(overAgents("call")).toBe(false)
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
    functions: [],
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
      "See, add, change and delete your Apple contacts, events, reminders and visits, and 1 more kind, including kinds added later, and search them by meaning, which spends your embedding budget"
    )
  })

  it("says a read sees, and a write may also add, change and delete", () => {
    const tasks = [kind("~/tasks/task", "Tasks")]
    expect(
      scopeSentence(scope({ pattern: "~/tasks/task", kinds: tasks }))
    ).toBe(
      "See your tasks, and search them by meaning, which spends your embedding budget"
    )
    expect(
      scopeSentence(
        scope({
          verb: "write",
          pattern: "~/tasks/task",
          kinds: tasks,
        })
      )
    ).toBe(
      "See, add, change and delete your tasks, and search them by meaning, which spends your embedding budget"
    )
  })

  it("names a kind with no label by its own words, and one with nothing installed yet by its pattern", () => {
    expect(
      scopeSentence(
        scope({
          pattern: "~/tasks/*",
          glob: true,
          kinds: [kind("~/tasks/task", "Tasks"), kind("~/tasks/project")],
        })
      )
    ).toBe(
      "See your tasks and projects, including kinds added later, and search them by meaning, which spends your embedding budget"
    )
    expect(
      scopeSentence(scope({ verb: "write", pattern: "~/notes/note" }))
    ).toBe(
      "See, add, change and delete your notes, and search them by meaning, which spends your embedding budget"
    )
    expect(scopeSentence(scope({ pattern: "~/notes/*", glob: true }))).toBe(
      "See your Notes records, including kinds added later, and search them by meaning, which spends your embedding budget"
    )
    expect(
      scopeSentence(scope({ pattern: "substrate.reamde.dev/llm/provider" }))
    ).toBe(
      "See your LLM providers, and search them by meaning, which spends your embedding budget"
    )
  })

  // The server refuses such a scope, so the sentence fixture never carries
  // one; the page still reads it without an empty package name.
  it("reads a pattern that names no package by its kind alone", () => {
    expect(scopeSentence(scope({ pattern: "note" }))).toBe(
      "See your notes, and search them by meaning, which spends your embedding budget"
    )
  })

  it("groups the kinds of several packages", () => {
    expect(
      scopeSentence(
        scope({
          pattern: "~/*",
          glob: true,
          kinds: [
            kind("~/people/person", "People"),
            kind("~/tasks/project", "Projects"),
            kind("~/tasks/task", "Tasks"),
          ],
        })
      )
    ).toBe(
      "See your people, and projects and tasks, including kinds added later, and search them by meaning, which spends your embedding budget"
    )
  })

  it("reads every record as every record", () => {
    expect(scopeSentence(scope({ pattern: "*", glob: true }))).toBe(
      "See all your records, including kinds added later, and search them by meaning, which spends your embedding budget"
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
          pattern: "~/llm/substrateEditor",
          agents: [agent("~/llm/substrateEditor")],
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
          pattern: "~/llm/substrateEditor",
          agents: [agent("~/llm/substrateEditor")],
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

  it("says a call runs the owner's code only as far as the app already reaches", () => {
    expect(
      scopeSentence(scope({ verb: "call", pattern: "*", glob: true }))
    ).toBe(
      "Run any of your functions, including functions added later; each runs only where the app may already see and change what it reads and writes"
    )
    expect(
      scopeSentence(scope({ verb: "call", pattern: "~/tasks/*", glob: true }))
    ).toBe(
      "Run the functions in Tasks, including functions added later; each runs only where the app may already see and change what it reads and writes"
    )
    expect(
      scopeSentence(
        scope({
          verb: "call",
          pattern: "~/tasks/*",
          glob: true,
          functions: [
            {
              identity: "~/tasks/summarize",
              reads: [],
              writes: [],
              covered: true,
            },
            {
              identity: "~/tasks/report",
              reads: [],
              writes: [],
              covered: false,
            },
          ],
        })
      )
    ).toBe(
      "Run Summarize and Report, including functions added later; each runs only where the app may already see and change what it reads and writes"
    )
    expect(
      scopeSentence(scope({ verb: "call", pattern: "~/tasks/summarize" }))
    ).toBe(
      "Run Summarize; it runs only where the app may already see and change what it reads and writes"
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
    ].map((n) => kind(`~/greek/${n}`, n[0].toUpperCase() + n.slice(1) + "s"))
    expect(scopeSentence(scope({ pattern: "~/greek/*", kinds }))).toBe(
      "See your Greek alphas, betas, gammas, deltas, epsilons and zetas, and 1 more kind, and search them by meaning, which spends your embedding budget"
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

/** A trait a trait scope names (decision 0225): a noun, as most are. */
const LIFECYCLE = "substrate.reamde.dev/core/lifecycle"

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
 * package's, one kind or agent, and a trait, with and without the kinds it
 * reaches. */
const SENTENCE_CASES: Omit<SentenceCase, "sentence">[] = [
  { scope: "read:*", kinds: [] },
  {
    scope: "read:~/*",
    kinds: [
      labelled("~/people/person", "Person", "People"),
      labelled("~/tasks/project", "Project", "Projects"),
      { identity: "~/tasks/task" },
    ],
  },
  { scope: "read:~/notes/*", kinds: [] },
  { scope: "read:~/*", kinds: [] },
  { scope: "read:b.example.com/*", kinds: [] },
  {
    scope: `read:${APPLEHEALTH}/account`,
    kinds: [
      labelled(`${APPLEHEALTH}/account`, "Account", "Accounts", "internal"),
    ],
  },
  { scope: "read:substrate.reamde.dev/llm/provider", kinds: [] },
  {
    scope: "read:~/greek/*",
    kinds: ["alpha", "beta", "gamma", "delta", "epsilon", "zeta", "eta"].map(
      (n) => {
        const word = n[0].toUpperCase() + n.slice(1)
        return labelled(`~/greek/${n}`, word, `${word}s`)
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
  { scope: "write:~/notes/note", kinds: [] },
  {
    scope: `write:${APPLEHEALTH}/account`,
    kinds: [
      labelled(
        `${APPLEHEALTH}/account`,
        "Apple Health account",
        "Apple Health accounts",
        "internal"
      ),
    ],
  },
  {
    scope: `read:implements(${LIFECYCLE})`,
    kinds: [{ identity: "~/tasks/task" }],
  },
  {
    scope: `write:implements(${LIFECYCLE})`,
    kinds: [
      { identity: "~/tasks/task" },
      labelled("~/tasks/project", "Project", "Projects"),
      labelled(
        "providers.substrate.reamde.dev/linear/issue",
        "Issue",
        "Issues"
      ),
      labelled(`${APPLE}/reminder`, "Reminder", "Reminders"),
    ],
  },
  { scope: `read:implements(${LIFECYCLE})`, kinds: [] },
  { scope: `write:implements(${LIFECYCLE})`, kinds: [] },
  {
    scope: "read:implements(~/scheduling/dueDate)",
    kinds: [],
  },
  { scope: "agents:*", kinds: [] },
  { scope: "agents:b.example.com/*", kinds: [] },
  { scope: "agents:~/*", kinds: [] },
  { scope: "agents:~/llm/*", kinds: [] },
  { scope: "agents:~/llm/substrateEditor", kinds: [] },
  { scope: "decide:*", kinds: [] },
  { scope: "decide:b.example.com/x/*", kinds: [] },
  { scope: "decide:~/llm/substrateEditor", kinds: [] },
  { scope: "call:*", kinds: [] },
  { scope: "call:b.example.com/*", kinds: [] },
  { scope: "call:~/*", kinds: [] },
  { scope: "call:~/tasks/*", kinds: [] },
  { scope: "call:~/tasks/summarize", kinds: [] },
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
    functions: [],
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

describe("a trait scope", () => {
  // Decision 0225: what `*` would reach among the kinds that bind the trait.
  it("is read off its pattern, and a kind pattern names no trait", () => {
    expect(scopeTrait(`implements(${LIFECYCLE})`)).toBe(LIFECYCLE)
    expect(scopeTrait("~/tasks/*")).toBeUndefined()
    expect(scopeTrait("implements()")).toBeUndefined()
  })

  it("names the trait, the kinds that have it today, and that more may come", () => {
    const sentence = (verb: "read" | "write", kinds: AuthorizationKind[]) =>
      scopeSentence({
        verb,
        pattern: `implements(${LIFECYCLE})`,
        glob: true,
        kinds,
        agents: [],
        functions: [],
      })
    expect(
      sentence("write", [
        { identity: "~/tasks/task" },
        {
          identity: "providers.substrate.reamde.dev/linear/issue",
          label: { singular: "Issue", plural: "Issues" },
        },
      ])
    ).toBe(
      "See, add, change and delete your records of every kind with the lifecycle trait (today your tasks, and Linear issues), including kinds that take it on later, and search them by meaning, which spends your embedding budget"
    )
    expect(sentence("read", [])).toBe(
      "See your records of every kind with the lifecycle trait (no kind has it yet), including kinds that take it on later, and search them by meaning, which spends your embedding budget"
    )
  })

  it("splits a camelCase trait name into words", () => {
    expect(
      scopeSentence({
        verb: "read",
        pattern: "implements(~/scheduling/dueDate)",
        glob: true,
        kinds: [],
        agents: [],
        functions: [],
      })
    ).toContain("every kind with the due date trait")
  })
})
