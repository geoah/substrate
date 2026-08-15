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
import type { KindInfo, SubstrateRecord } from "@/lib/api/types"

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

import { templateYAML } from "@/lib/record-yaml"
import { RecordEditorForm } from "./record-editor"

const taskKind: KindInfo = {
  identity: "tasks.substrate.reamde.dev/task",
  name: "task",
  authority: "tasks.substrate.reamde.dev",
  version: 0,
  plural: "tasks",
  source: "installed",
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
  kind: "tasks.substrate.reamde.dev/task",
  properties: { title: "write it", status: "open" },
  labels: {},
  version: 2,
  createdAt: "x",
  updatedAt: "x",
}

function renderEditor(
  over: {
    mode?: "create" | "edit"
    record?: SubstrateRecord
    seed?: string
  } = {}
) {
  const mode = over.mode ?? "create"
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <Toaster>
        <RecordEditorForm
          authority="tasks.substrate.reamde.dev"
          plural="tasks"
          mode={mode}
          kind={taskKind}
          kinds={[taskKind]}
          record={over.record}
          seed={over.seed ?? templateYAML(taskKind)}
        />
      </Toaster>
    </QueryClientProvider>
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

describe("the record editor", () => {
  it("opens on the form lens, composed from the declaration", () => {
    renderEditor()
    expect(screen.getByLabelText(/^Title/)).toBeTruthy()
    expect(screen.getByText("when it is due")).toBeTruthy()
    // The required blank is already named, before anything is applied.
    expect(screen.getByText(/`title` is required/)).toBeTruthy()
  })

  it("carries an edit between the lenses: one document, two views", async () => {
    renderEditor()
    fireEvent.change(screen.getByLabelText(/^Title/), {
      target: { value: "write the editor" },
    })
    fireEvent.click(screen.getByRole("tab", { name: "YAML" }))
    const yaml = await yamlLens()
    expect(yaml.text()).toContain("title: write the editor")
    // ...and back, with a YAML edit showing up on the form.
    yaml.replace(yaml.text().replace("write the editor", "renamed"))
    fireEvent.click(screen.getByRole("tab", { name: "Form" }))
    expect((screen.getByLabelText(/^Title/) as HTMLInputElement).value).toBe(
      "renamed"
    )
  })

  it("names a datatype problem before the apply, and bars the save", async () => {
    renderEditor()
    fireEvent.change(screen.getByLabelText(/^Title/), {
      target: { value: "hi" },
    })
    fireEvent.change(screen.getByLabelText(/Due at/), {
      target: { value: "yesterday" },
    })
    // The control says it on the field...
    expect(screen.getByText(/expected a timestamp/)).toBeTruthy()
    // ...and a value the document never took cannot bar the save, so put a
    // bad one in through the YAML lens, which is where a hand edit lands.
    fireEvent.click(screen.getByRole("tab", { name: "YAML" }))
    const yaml = await yamlLens()
    yaml.replace(yaml.text().replace('dueAt: ""', "dueAt: yesterday"))
    await waitFor(() =>
      expect(screen.getByText(/`dueAt`: expected a timestamp/)).toBeTruthy()
    )
    expect(
      (screen.getByRole("button", { name: "Create" }) as HTMLButtonElement)
        .disabled
    ).toBe(true)
    expect(createRecord).not.toHaveBeenCalled()
  })

  it("writes the DECLARED types, and leaves the blanks it never filled in", async () => {
    createRecord.mockResolvedValue({ ...openTask, id: "t9" })
    renderEditor()
    fireEvent.change(screen.getByLabelText(/^Title/), {
      target: { value: "hi" },
    })
    fireEvent.change(screen.getByLabelText(/^Effort/), {
      target: { value: "3" },
    })
    fireEvent.click(screen.getByRole("button", { name: "Create" }))
    await waitFor(() => expect(createRecord).toHaveBeenCalled())
    const [authority, plural, input] = createRecord.mock.calls[0]
    expect(authority).toBe("tasks.substrate.reamde.dev")
    expect(plural).toBe("tasks")
    expect(input.properties).toEqual({
      title: "hi",
      effort: 3,
      status: "open",
    })
    expect(input.id).toBeUndefined()
  })

  it("refuses to move a state on an edit, because a put may not", async () => {
    renderEditor({
      mode: "edit",
      record: openTask,
      seed: "kind: tasks.substrate.reamde.dev/task\nmetadata:\n  id: t1\ndata:\n  properties:\n    title: write it\n    status: open\n",
    })
    fireEvent.click(screen.getByRole("tab", { name: "YAML" }))
    const yaml = await yamlLens()
    yaml.replace(yaml.text().replace("status: open", "status: done"))
    await waitFor(() =>
      expect(screen.getByText(/moves by transition/)).toBeTruthy()
    )
    expect(
      (
        screen.getByRole("button", {
          name: "Save changes",
        }) as HTMLButtonElement
      ).disabled
    ).toBe(true)
  })

  it("formats the document on demand, in the lens that has one", async () => {
    renderEditor({
      seed: "kind: tasks.substrate.reamde.dev/task\ndata:\n      properties:\n            title: hi\n",
    })
    expect(screen.queryByRole("button", { name: /Format/ })).toBeNull()
    fireEvent.click(screen.getByRole("tab", { name: "YAML" }))
    fireEvent.click(screen.getByRole("button", { name: /Format/ }))
    expect((await yamlLens()).text()).toContain("    title: hi")
  })
})
