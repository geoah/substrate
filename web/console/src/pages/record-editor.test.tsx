// @vitest-environment jsdom
/** The record editor as the owner meets it: two lenses over one document.
 *
 * The contract under test is the one the old surface broke — that what you type
 * is checked against the DECLARATION before the apply, that the form and the
 * YAML are the same document (so switching loses nothing), and that a write
 * carries the declared TYPES rather than whatever the textarea held. */

import { EditorView } from "@codemirror/view"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react"
import { afterEach, beforeAll, describe, expect, it, vi } from "vitest"

import { Toaster } from "@/components/ui/toast"
import { ApiError, type KindInfo, type SubstrateRecord } from "@/lib/api/types"

vi.mock("@tanstack/react-router", () => ({
  Link: ({
    to,
    params,
    children,
    ...rest
  }: {
    to: string
    params?: Record<string, string>
    children: React.ReactNode
  } & React.AnchorHTMLAttributes<HTMLAnchorElement>) => (
    <a data-to={to} data-params={JSON.stringify(params ?? {})} {...rest}>
      {children}
    </a>
  ),
  useNavigate: () => vi.fn(),
}))

vi.mock("@/router", () => ({
  recordNewRoute: { useParams: () => ({}) },
  recordEditRoute: { useParams: () => ({}) },
}))

const createRecord = vi.fn()
const putRecord = vi.fn()
vi.mock("@/lib/api/records", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/api/records")>()),
  createRecord: (...args: unknown[]) => createRecord(...args),
  putRecord: (...args: unknown[]) => putRecord(...args),
}))

import { ConsolePreferencesContext } from "@/hooks/use-console-preferences"
import { DEFAULT_SETTINGS } from "@/lib/console-preferences"
import { templateYAML } from "@/lib/record-yaml"
import { RecordEditorForm } from "./record-editor"
import { SHIPPED_UNITS } from "@/lib/unit-graph.test-fixtures"
import { expectNoTrace } from "@/hooks/use-secret-mutation.test-fixtures"

const taskKind: KindInfo = {
  identity: "samples.substrate.reamde.dev/tasks/task",
  name: "task",
  authority: "samples.substrate.reamde.dev",
  package: "tasks",
  version: 0,
  source: "installed",
  description: "",
  definition: {
    properties: {
      title: { type: "string", required: true, description: "what to do" },
      dueAt: { type: "datetime", description: "when it is due" },
      effort: { type: "int" },
      status: {
        type: "state",
        states: ["proposed", "open", "done"],
        initial: "open",
      },
    },
  },
}

const openTask: SubstrateRecord = {
  id: "t1",
  kind: "samples.substrate.reamde.dev/tasks/task",
  properties: { title: "write it", status: "open" },
  labels: {},
  version: 2,
  createdAt: "x",
  updatedAt: "x",
}

const EDIT_SEED =
  "kind: samples.substrate.reamde.dev/tasks/task\nmetadata:\n  id: t1\ndata:\n  properties:\n    title: write it\n    status: open\n"

function renderEditor(
  over: {
    mode?: "create" | "edit"
    record?: SubstrateRecord
    seed?: string
    technical?: boolean
    kind?: KindInfo
  } = {}
) {
  const mode = over.mode ?? "create"
  const kind = over.kind ?? taskKind
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  const view = render(
    <ConsolePreferencesContext.Provider
      value={{
        preferences: {
          collapsed: [],
          favorites: [],
          sidebarOpen: true,
          ...DEFAULT_SETTINGS,
          technicalDetails: over.technical ?? false,
        },
        busy: false,
        change: () => {},
        set: () => {},
      }}
    >
      <QueryClientProvider client={client}>
        <Toaster>
          <RecordEditorForm
            authority="samples.substrate.reamde.dev"
            pkg="tasks"
            name="task"
            mode={mode}
            kind={kind}
            kinds={[kind]}
            record={over.record}
            seed={over.seed ?? templateYAML(SHIPPED_UNITS, kind)}
          />
        </Toaster>
      </QueryClientProvider>
    </ConsolePreferencesContext.Provider>
  )
  return { ...view, client }
}

/** The YAML lens is CodeMirror, so a test drives it the way any other client
 * would: through the view's own document, found from the DOM node the editor
 * owns. */
async function yamlLens() {
  const dom = await screen.findByLabelText("Record YAML")
  const view = EditorView.findFromDOM(dom)
  if (!view) throw new Error("the YAML lens is not mounted")
  return {
    text: () => view.state.doc.toString(),
    replace: (next: string) =>
      act(() => {
        view.dispatch({
          changes: { from: 0, to: view.state.doc.length, insert: next },
        })
      }),
  }
}

// The YAML lens is a React.lazy chunk (CodeMirror and its grammar). Loading it
// once here keeps that first transform out of the timeout of whichever test
// opens the lens first, which a loaded machine otherwise runs past.
beforeAll(async () => {
  await import("@/components/record/yaml-editor")
}, 30_000)

afterEach(() => {
  cleanup()
  createRecord.mockReset()
  putRecord.mockReset()
})

describe("the new-record sheet", () => {
  it("opens as the record it will become: a title, rows, and the rest folded", () => {
    renderEditor()
    expect(screen.getByLabelText(/^Title/)).toBeTruthy()
    // The time it is about is asked for; the rest folds.
    expect(screen.getByRole("button", { name: /^Due at .*edit$/ })).toBeTruthy()
    expect(screen.queryByRole("button", { name: /^Effort .*edit$/ })).toBeNull()
    fireEvent.click(
      screen.getByRole("button", { name: /2 more: Status, Effort/ })
    )
    expect(screen.getByRole("button", { name: /^Effort .*edit$/ })).toBeTruthy()
    // Nothing is named wrong before the person asks to create.
    expect(screen.queryAllByRole("alert")).toEqual([])
  })

  it("names what is missing when asked to create, and creates nothing", () => {
    renderEditor()
    fireEvent.click(screen.getByRole("button", { name: "Create task" }))
    expect(screen.getByRole("alert").textContent).toBe("Title is required.")
    expect(createRecord).not.toHaveBeenCalled()
  })

  it("names a problem once the value it is about moved, not before", () => {
    renderEditor()
    fireEvent.change(screen.getByLabelText(/^Title/), {
      target: { value: "hi" },
    })
    fireEvent.change(screen.getByLabelText(/^Title/), {
      target: { value: "" },
    })
    expect(screen.getByRole("alert").textContent).toBe("Title is required.")
  })

  it("carries an edit between the sheet and the YAML: one document", async () => {
    renderEditor({ technical: true })
    fireEvent.change(screen.getByLabelText(/^Title/), {
      target: { value: "write the editor" },
    })
    fireEvent.click(screen.getByRole("button", { name: "Write YAML" }))
    const yaml = await yamlLens()
    expect(yaml.text()).toContain("title: write the editor")
    yaml.replace(yaml.text().replace("write the editor", "renamed"))
    fireEvent.click(screen.getByRole("button", { name: "Use the form" }))
    expect(screen.getByLabelText<HTMLInputElement>(/^Title/).value).toBe(
      "renamed"
    )
  })

  it("offers the YAML only in technical mode", () => {
    renderEditor()
    expect(screen.queryByRole("button", { name: "Write YAML" })).toBeNull()
  })

  it("names a datatype problem on its row and in the YAML, and bars the create", async () => {
    renderEditor({ technical: true })
    fireEvent.change(screen.getByLabelText(/^Title/), {
      target: { value: "hi" },
    })
    fireEvent.click(screen.getByRole("button", { name: "Write YAML" }))
    const yaml = await yamlLens()
    yaml.replace(yaml.text().replace('dueAt: ""', "dueAt: yesterday"))
    await waitFor(() =>
      expect(screen.getByText(/`dueAt`: expected a timestamp/)).toBeTruthy()
    )
    fireEvent.click(screen.getByRole("button", { name: "Use the form" }))
    expect(screen.getByRole("alert").textContent).toMatch(
      /^Expected a timestamp/
    )
    fireEvent.click(screen.getByRole("button", { name: "Create task" }))
    expect(createRecord).not.toHaveBeenCalled()
  })

  it("writes the DECLARED types, and leaves the blanks it never filled in", async () => {
    createRecord.mockResolvedValue({ ...openTask, id: "t9" })
    renderEditor()
    fireEvent.change(screen.getByLabelText(/^Title/), {
      target: { value: "hi" },
    })
    fireEvent.click(screen.getByRole("button", { name: /more:/ }))
    fireEvent.click(screen.getByRole("button", { name: /^Effort .*edit$/ }))
    const effort = screen.getByLabelText("Effort")
    fireEvent.change(effort, { target: { value: "3" } })
    fireEvent.keyDown(effort, { key: "Enter" })
    await waitFor(() =>
      expect(
        document.querySelector("[data-property=effort]")?.textContent
      ).toContain("3")
    )
    fireEvent.click(screen.getByRole("button", { name: "Create task" }))
    await waitFor(() => expect(createRecord).toHaveBeenCalled())
    const [authority, pkg, name, input] = createRecord.mock.calls[0]
    expect(authority).toBe("samples.substrate.reamde.dev")
    expect(pkg).toBe("tasks")
    expect(name).toBe("task")
    expect(input.properties).toEqual({
      title: "hi",
      effort: 3,
      status: "open",
    })
    expect(input.id).toBeUndefined()
  })

  it("leaves an optional number and text the person never touched out of the write", async () => {
    const paperKind: KindInfo = {
      ...taskKind,
      identity: "samples.substrate.reamde.dev/papers/paper",
      name: "paper",
      package: "papers",
      definition: {
        properties: {
          title: { type: "string", required: true },
          year: { type: "int", min: 1000 },
          abstract: { type: "markdown" },
        },
      },
    }
    createRecord.mockResolvedValue({ ...openTask, id: "p1" })
    renderEditor({ kind: paperKind })
    fireEvent.change(screen.getByLabelText(/^Title/), {
      target: { value: "Year left alone" },
    })
    fireEvent.click(screen.getByRole("button", { name: "Create paper" }))
    await waitFor(() => expect(createRecord).toHaveBeenCalled())
    expect(screen.queryAllByRole("alert")).toEqual([])
    expect(createRecord.mock.calls[0][3].properties).toEqual({
      title: "Year left alone",
    })
  })

  it("says why the server refused, in words", async () => {
    createRecord.mockRejectedValue(
      new ApiError("validation", "project must exist", 422)
    )
    renderEditor()
    fireEvent.change(screen.getByLabelText(/^Title/), {
      target: { value: "hi" },
    })
    fireEvent.click(screen.getByRole("button", { name: "Create task" }))
    await waitFor(() =>
      expect(screen.getByRole("alert").textContent).toContain(
        "Couldn’t save: project must exist"
      )
    )
  })

  it("formats the document on demand, in the YAML", async () => {
    renderEditor({
      technical: true,
      seed: "kind: samples.substrate.reamde.dev/tasks/task\ndata:\n      properties:\n            title: hi\n",
    })
    expect(screen.queryByRole("button", { name: /Format/ })).toBeNull()
    fireEvent.click(screen.getByRole("button", { name: "Write YAML" }))
    fireEvent.click(screen.getByRole("button", { name: /Format/ }))
    expect((await yamlLens()).text()).toContain("    title: hi")
  })
})

describe("the record editor (edit)", () => {
  it("opens an edit on the YAML, and keeps the form a tab away", async () => {
    renderEditor({ mode: "edit", record: openTask, seed: EDIT_SEED })
    expect((await yamlLens()).text()).toContain("title: write it")
    fireEvent.click(screen.getByRole("tab", { name: "Form" }))
    expect(screen.getByLabelText<HTMLInputElement>(/^Title/).value).toBe(
      "write it"
    )
  })

  it("refuses to move a state on an edit, because a put may not", async () => {
    renderEditor({ mode: "edit", record: openTask, seed: EDIT_SEED })
    const yaml = await yamlLens()
    yaml.replace(yaml.text().replace("status: open", "status: done"))
    await waitFor(() =>
      expect(screen.getByText(/changes by transition/)).toBeTruthy()
    )
    expect(
      screen.getByRole<HTMLButtonElement>("button", {
        name: "Save changes",
      }).disabled
    ).toBe(true)
  })
})

describe("a secret typed into the editor", () => {
  const credentialKind: KindInfo = {
    ...taskKind,
    identity: "samples.substrate.reamde.dev/vault/credential",
    name: "credential",
    package: "vault",
    definition: {
      properties: {
        title: { type: "string", required: true },
        token: { type: "secret", required: true },
      },
    },
  }
  const stored: SubstrateRecord = {
    id: "c1",
    kind: credentialKind.identity,
    properties: { title: "Mail", token: "[redacted]" },
    labels: {},
    version: 2,
    createdAt: "x",
    updatedAt: "x",
  }
  const keyBox = () =>
    document.querySelector<HTMLInputElement>("input[type=password]")

  it("leaves nothing of it in the mutation cache after the sheet takes it, a refused create, a create or Esc", async () => {
    const { client } = renderEditor({ kind: credentialKind })
    fireEvent.change(screen.getByLabelText(/^Title/), {
      target: { value: "Mail" },
    })
    const typeToken = (secret: string, key: string) => {
      fireEvent.click(screen.getByRole("button", { name: /^Token .*edit$/ }))
      fireEvent.change(keyBox()!, { target: { value: secret } })
      fireEvent.keyDown(keyBox()!, { key })
    }

    typeToken("sk-sheet-cancelled", "Escape")
    expect(keyBox()).toBeNull()
    expectNoTrace(client, "sk-sheet-cancelled")

    typeToken("sk-sheet-secret", "Enter")
    await waitFor(() => expect(keyBox()).toBeNull())
    expectNoTrace(client, "sk-sheet-secret")

    createRecord.mockRejectedValueOnce(
      new ApiError("validation", "token is not accepted", 422)
    )
    fireEvent.click(screen.getByRole("button", { name: "Create credential" }))
    await waitFor(() =>
      expect(screen.getByRole("alert").textContent).toContain(
        "token is not accepted"
      )
    )
    expect(createRecord.mock.calls[0][3].properties.token).toBe(
      "sk-sheet-secret"
    )
    expectNoTrace(client, "sk-sheet-secret")

    createRecord.mockResolvedValueOnce({ ...stored, id: "c9" })
    fireEvent.click(screen.getByRole("button", { name: "Create credential" }))
    await waitFor(() => expect(createRecord).toHaveBeenCalledTimes(2))
    await waitFor(() =>
      expect(
        screen.getByRole<HTMLButtonElement>("button", {
          name: "Create credential",
        }).disabled
      ).toBe(false)
    )
    expectNoTrace(client, "sk-sheet-secret")
  })

  // The sheet's record is the draft as typed so far, the token included: an
  // ordinary row's write must not keep that record in the cache either.
  it("leaves nothing of it in the mutation cache after another row is edited and the sheet closes", async () => {
    const loginKind: KindInfo = {
      ...credentialKind,
      definition: {
        properties: {
          title: { type: "string", required: true },
          token: { type: "secret", required: true },
          username: { type: "string", required: true },
        },
      },
    }
    const { client, unmount } = renderEditor({ kind: loginKind })
    fireEvent.change(screen.getByLabelText(/^Title/), {
      target: { value: "Mail" },
    })
    fireEvent.click(screen.getByRole("button", { name: /^Token .*edit$/ }))
    fireEvent.change(keyBox()!, { target: { value: "sk-draft-secret" } })
    fireEvent.keyDown(keyBox()!, { key: "Enter" })
    await waitFor(() => expect(keyBox()).toBeNull())

    fireEvent.click(screen.getByRole("button", { name: /^Username .*edit$/ }))
    const username = screen.getByRole("textbox", { name: "Username" })
    fireEvent.change(username, { target: { value: "ada" } })
    fireEvent.keyDown(username, { key: "Enter" })
    await waitFor(() =>
      expect(screen.queryByRole("textbox", { name: "Username" })).toBeNull()
    )

    // Both rows' writes landed in the draft the create sends.
    createRecord.mockResolvedValueOnce({ ...stored, id: "c9" })
    fireEvent.click(screen.getByRole("button", { name: "Create credential" }))
    await waitFor(() => expect(createRecord).toHaveBeenCalledTimes(1))
    expect(createRecord.mock.calls[0][3].properties).toEqual({
      title: "Mail",
      token: "sk-draft-secret",
      username: "ada",
    })

    unmount()
    expectNoTrace(client, "sk-draft-secret")
  })

  it("leaves nothing of it in the mutation cache after an edit's save or refusal", async () => {
    const { client } = renderEditor({
      mode: "edit",
      kind: credentialKind,
      record: stored,
      seed: "kind: samples.substrate.reamde.dev/vault/credential\nmetadata:\n  id: c1\ndata:\n  properties:\n    title: Mail\n",
    })
    const yaml = await yamlLens()
    yaml.replace(`${yaml.text()}    token: sk-yaml-secret\n`)
    const save = await screen.findByRole<HTMLButtonElement>("button", {
      name: "Save changes",
    })
    await waitFor(() => expect(save.disabled).toBe(false))

    putRecord.mockRejectedValueOnce(
      new ApiError("conflict", "version mismatch", 409)
    )
    fireEvent.click(save)
    await waitFor(() => expect(putRecord).toHaveBeenCalledTimes(1))
    expect(putRecord.mock.calls[0][4].properties.token).toBe("sk-yaml-secret")
    await waitFor(() => expect(save.disabled).toBe(false))
    expectNoTrace(client, "sk-yaml-secret")

    putRecord.mockResolvedValueOnce(stored)
    fireEvent.click(save)
    await waitFor(() => expect(putRecord).toHaveBeenCalledTimes(2))
    await waitFor(() => expect(save.disabled).toBe(false))
    expectNoTrace(client, "sk-yaml-secret")
  })
})
