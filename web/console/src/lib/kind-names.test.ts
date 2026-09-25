import { describe, expect, it } from "vitest"

import type { KindInfo } from "@/lib/api/types"
import {
  displayName,
  displayPlural,
  lowerFirst,
  packageDisplayName,
  pluralWord,
  splitWords,
  untitled,
} from "./kind-names"

describe("kind display names", () => {
  // Real `names.singular` values from kinds/ and samples/.
  it.each([
    ["person", "Person", "People"],
    ["task", "Task", "Tasks"],
    ["project", "Project", "Projects"],
    ["calendarevent", "Calendar event", "Calendar events"],
    ["calendareventseries", "Calendar event series", "Calendar event series"],
    ["calendarseries", "Calendar series", "Calendar series"],
    ["calendarsync", "Calendar sync", "Calendar syncs"],
    ["gmailthread", "Gmail thread", "Gmail threads"],
    ["gmailattachment", "Gmail attachment", "Gmail attachments"],
    ["gmaillabel", "Gmail label", "Gmail labels"],
    ["drivefile", "Drive file", "Drive files"],
    ["emailaddress", "Email address", "Email addresses"],
    ["emailmessage", "Email message", "Email messages"],
    ["conversationmessage", "Conversation message", "Conversation messages"],
    ["contactgroup", "Contact group", "Contact groups"],
    ["chataccount", "Chat account", "Chat accounts"],
    ["datasource", "Data source", "Data sources"],
    ["database", "Database", "Databases"],
    ["pullrequest", "Pull request", "Pull requests"],
    ["issuetype", "Issue type", "Issue types"],
    ["projectstatus", "Project status", "Project statuses"],
    ["workflowstate", "Workflow state", "Workflow states"],
    ["webdocument", "Web document", "Web documents"],
    ["recordmergerequest", "Record merge request", "Record merge requests"],
    ["recordpatchpolicy", "Record patch policy", "Record patch policies"],
    ["recoverykey", "Recovery key", "Recovery keys"],
    ["consolepreference", "Console preference", "Console preferences"],
    ["codeofconduct", "Code of conduct", "Codes of conduct"],
    ["repository", "Repository", "Repositories"],
    ["tasklog", "Task log", "Task logs"],
    ["organization", "Organization", "Organizations"],
    ["recording", "Recording", "Recordings"],
    ["propertytype", "Property type", "Property types"],
  ])("%s → %s / %s", (name, singular, plural) => {
    expect(displayName(name)).toBe(singular)
    expect(displayPlural(name)).toBe(plural)
  })

  it.each([
    ["apikey", "API key", "API keys"],
    ["llmprovider", "LLM provider", "LLM providers"],
    ["oauthclient", "OAuth client", "OAuth clients"],
    ["weburl", "Web URL", "Web URLs"],
    ["htmlpage", "HTML page", "HTML pages"],
    ["userid", "User ID", "User IDs"],
  ])("reads acronyms in capitals: %s → %s / %s", (name, one, many) => {
    expect(displayName(name)).toBe(one)
    expect(displayPlural(name)).toBe(many)
  })

  it("names a package by its word, acronyms in capitals", () => {
    expect(packageDisplayName("llm")).toBe("LLM")
    expect(packageDisplayName("tasks")).toBe("Tasks")
    expect(packageDisplayName("google")).toBe("Google")
    expect(packageDisplayName("oauth")).toBe("OAuth")
    expect(packageDisplayName("github")).toBe("GitHub")
  })

  it("lowers a display name for a sentence, keeping acronyms and brands", () => {
    expect(lowerFirst("Calendar events")).toBe("calendar events")
    expect(lowerFirst("API keys")).toBe("API keys")
    expect(lowerFirst("LLM providers")).toBe("LLM providers")
    expect(lowerFirst("Gmail threads")).toBe("Gmail threads")
    expect(lowerFirst("OAuth client")).toBe("OAuth client")
  })

  it("reads the name off a full kind reference", () => {
    expect(displayPlural("samples.substrate.reamde.dev/people/person")).toBe(
      "People"
    )
    expect(
      displayName("providers.substrate.reamde.dev/google/gmailthread")
    ).toBe("Gmail thread")
  })

  it("prefers the declaration's names.singular on a registry entry", () => {
    const kind = {
      identity: "example.com/trips/trip",
      name: "trip",
      authority: "example.com",
      package: "trips",
      version: 1,
      source: "installed",
      description: "",
      definition: { names: { singular: "calendarevent" } },
    } satisfies KindInfo
    expect(displayName(kind)).toBe("Calendar event")
  })

  it("keeps a name it cannot split whole", () => {
    expect(splitWords("frobnicator")).toBeUndefined()
    expect(displayName("frobnicator")).toBe("Frobnicator")
    expect(displayPlural("frobnicator")).toBe("Frobnicators")
  })

  it("pluralises one word by its ending", () => {
    expect(pluralWord("key")).toBe("keys")
    expect(pluralWord("policy")).toBe("policies")
    expect(pluralWord("match")).toBe("matches")
    expect(pluralWord("box")).toBe("boxes")
    expect(pluralWord("Person")).toBe("People")
    expect(pluralWord("series")).toBe("series")
  })

  it("names a record without a title by its kind", () => {
    expect(untitled("samples.substrate.reamde.dev/people/person")).toBe(
      "Untitled person"
    )
    expect(untitled("providers.substrate.reamde.dev/google/gmailthread")).toBe(
      "Untitled Gmail thread"
    )
  })
})
