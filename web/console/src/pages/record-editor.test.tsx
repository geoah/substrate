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
import { afterEach, describe, expect, it, vi } from "vitest"

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
  } = {}
) {
  const mode = over.mode ?? "create"
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
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
            kind={taskKind}
            kinds={[taskKind]}
            record={over.record}
            seed={over.seed ?? templateYAML(taskKind)}
          />
        </Toaster>
      </QueryClientProvider>
    </ConsolePreferencesContext.Provider>
  )
}

/** The YAML lens is CodeMirror, so a test drives it the way any other client
 * would: through the view's own document, found from the DOM node the editor
 * owns. */
async function yamlLens() {
  const dom = await screen.findByLabelText("Record YAML")
  const view = EditorView.findFromDOM(dom as HTMLElement)
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
    expect((screen.getByLabelText(/^Title/) as HTMLInputElement).value).toBe(
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
    expect((screen.getByLabelText(/^Title/) as HTMLInputElement).value).toBe(
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
      (
        screen.getByRole("button", {
          name: "Save changes",
        }) as HTMLButtonElement
      ).disabled
    ).toBe(true)
  })
})
