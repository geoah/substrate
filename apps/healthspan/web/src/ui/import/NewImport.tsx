import {
  ActionRow,
  BarButton,
  ProblemBox,
  Section,
  Spinner,
  cx,
} from "@substrate/react"
import { describe } from "@substrate/client"
import { FileUp } from "lucide-react"
import { useEffect, useRef, useState, type DragEvent } from "react"
import { templateCsv } from "../../import/columns"
import type { Table } from "../../import/csv"
import { tableFile, tableOf } from "../../import/importers/csv"
import {
  inputFromDirectoryEntries,
  inputFromFiles,
} from "../../import/importers/input"
import type { Importer, Input } from "../../import/importers/types"
import { TextButton } from "../IconButton"
import { EnterResultSheet } from "./EnterResultSheet"
import { CsvImport } from "./ImportWizard"
import { ProviderImport } from "./ProviderImport"
import { detectAll, fileImporters, type Detected } from "./importers"

function download(name: string, text: string) {
  const url = URL.createObjectURL(new Blob([text], { type: "text/csv" }))
  const a = document.createElement("a")
  a.href = url
  a.download = name
  document.body.append(a)
  a.click()
  a.remove()
  setTimeout(() => URL.revokeObjectURL(url), 1000)
}

/** Every importer that reads files, loaded once for the screen. */
function useFileImporters(): Importer[] | undefined {
  const [list, setList] = useState<Importer[]>()
  useEffect(() => {
    let live = true
    void fileImporters().then((l) => {
      if (live) setList(l)
    })
    return () => {
      live = false
    }
  }, [])
  return list
}

/** The first step: files, a folder or a zip, chosen or dropped; what each
 * importer takes; the CSV template; and a result typed in instead. */
function ChooseInput(props: {
  importers: Importer[] | undefined
  onInput: (input: Promise<Input>) => void
  problem?: string
}) {
  const [over, setOver] = useState(false)
  const [entering, setEntering] = useState(false)
  const files = useRef<HTMLInputElement>(null)
  const folder = useRef<HTMLInputElement>(null)
  const onDrop = (e: DragEvent) => {
    e.preventDefault()
    setOver(false)
    const entries = [...e.dataTransfer.items]
      .filter((i) => i.kind === "file")
      .map((i) => i.webkitGetAsEntry?.())
      .filter((x): x is FileSystemEntry => !!x)
    if (entries.length) props.onInput(inputFromDirectoryEntries(entries))
    else if (e.dataTransfer.files.length)
      props.onInput(inputFromFiles([...e.dataTransfer.files]))
  }
  const picked = (list: FileList | null) => {
    if (list?.length) props.onInput(inputFromFiles([...list]))
  }
  return (
    <>
      <div
        onDragOver={(e) => {
          e.preventDefault()
          setOver(true)
        }}
        onDragLeave={() => setOver(false)}
        onDrop={onDrop}
        className={cx(
          "mb-7 flex flex-col items-center gap-2 rounded-[14px] border-2 border-dashed px-6 py-8 text-center",
          over ? "border-accent bg-accent/10" : "border-separator bg-card"
        )}
      >
        <FileUp className="size-8 text-accent-text" aria-hidden />
        <span className="text-[17px] font-semibold">Choose what to import</span>
        <span className="text-[15px] text-secondary">
          A file, several files, a folder or a zip of one. Or drop it here.
        </span>
        <div className="mt-2 flex flex-wrap justify-center gap-2">
          <BarButton prominent onClick={() => files.current?.click()}>
            Choose Files…
          </BarButton>
          <BarButton onClick={() => folder.current?.click()}>
            Choose a Folder…
          </BarButton>
        </div>
        <input
          ref={files}
          type="file"
          multiple
          className="sr-only"
          tabIndex={-1}
          aria-hidden
          onChange={(e) => {
            picked(e.target.files)
            e.target.value = ""
          }}
        />
        <input
          ref={(el) => {
            folder.current = el
            el?.setAttribute("webkitdirectory", "")
          }}
          type="file"
          className="sr-only"
          tabIndex={-1}
          aria-hidden
          onChange={(e) => {
            picked(e.target.files)
            e.target.value = ""
          }}
        />
      </div>
      {props.problem && (
        <Section>
          <ProblemBox
            role="alert"
            title="Couldn't read it"
            message={props.problem}
          />
        </Section>
      )}
      <Section
        header="What it reads"
        footer="Nothing is written until you have seen what an import writes and chosen Import."
      >
        {(props.importers ?? []).map((i) => (
          <div key={i.id} className="flex flex-col gap-0.5 px-4 py-2.5">
            <span className="text-[17px] break-words">{i.name}</span>
            <span className="text-[13px] break-words text-secondary">
              {i.description}
            </span>
          </div>
        ))}
        {!props.importers && (
          <div className="flex justify-center py-4 text-secondary">
            <Spinner />
          </div>
        )}
        <ActionRow
          onClick={() => download("lab-results-template.csv", templateCsv())}
        >
          Download the CSV Template
        </ActionRow>
        <ActionRow onClick={() => setEntering(true)}>
          Enter One Result Instead…
        </ActionRow>
      </Section>
      <EnterResultSheet open={entering} onClose={() => setEntering(false)} />
    </>
  )
}

/** What the input is: the importers that take it, the surest first, each
 * with why, and the owner's pick. */
function ChooseImporter(props: {
  input: Input
  found: Detected[]
  onPick: (d: Detected) => void
  onBack: () => void
}) {
  const [best, ...rest] = props.found
  const count = props.input.files.length
  return (
    <>
      <Section>
        <div className="flex flex-col gap-0.5 px-4 py-2.5">
          <span className="text-[17px] break-words">
            {props.input.name || "What you chose"}
          </span>
          <span className="text-[15px] text-secondary">
            {`${count.toLocaleString("en-GB")} ${count === 1 ? "file" : "files"}`}
          </span>
        </div>
        <ActionRow onClick={props.onBack}>Choose Something Else…</ActionRow>
      </Section>
      {best ? (
        <>
          <Section header="It looks like">
            <div className="flex flex-col gap-0.5 px-4 py-2.5">
              <span className="text-[17px] font-semibold break-words">
                {best.importer.name}
              </span>
              <span className="text-[15px] break-words text-secondary">
                {best.match.why}
              </span>
            </div>
            <div className="px-4 pb-3">
              <BarButton prominent onClick={() => props.onPick(best)}>
                Continue
              </BarButton>
            </div>
          </Section>
          {rest.length > 0 && (
            <Section header="Or read it as">
              {rest.map((d) => (
                <button
                  key={d.importer.id}
                  type="button"
                  onClick={() => props.onPick(d)}
                  className="flex min-h-11 w-full flex-col gap-0.5 px-4 py-2.5 text-left active:bg-selected"
                >
                  <span className="text-[17px] break-words text-accent-text">
                    {d.importer.name}
                  </span>
                  <span className="text-[13px] break-words text-secondary">
                    {d.match.why}
                  </span>
                </button>
              ))}
            </Section>
          )}
        </>
      ) : (
        <Section>
          <ProblemBox
            role="alert"
            title="Nothing here reads it"
            message="No importer takes these files as its own. Choose a CSV of lab results, or a provider's export as it downloads."
            action={{ title: "Choose Something Else", onClick: props.onBack }}
          />
        </Section>
      )}
    </>
  )
}

type Step =
  | { step: "choose"; problem?: string }
  | { step: "reading" }
  | { step: "detected"; input: Input; found: Detected[] }
  | { step: "csv"; input: Input; table: Table }
  | { step: "provider"; input: Input; importer: Importer }

/** A new import (docs/plans/healthspan.md, Import): what to import, which
 * importer reads it, its questions, its plan, then Import. */
export function NewImport() {
  const importers = useFileImporters()
  const [state, setState] = useState<Step>({ step: "choose" })
  const reading = useRef(0)
  const back = () => {
    reading.current++
    setState({ step: "choose" })
  }

  const onInput = async (pending: Promise<Input>) => {
    const run = ++reading.current
    setState({ step: "reading" })
    try {
      const input = await pending
      if (!input.files.length) {
        setState({ step: "choose", problem: "It holds no files." })
        return
      }
      const found = await detectAll(input, importers ?? (await fileImporters()))
      if (run !== reading.current) return
      setState({ step: "detected", input, found })
    } catch (e) {
      if (run === reading.current)
        setState({ step: "choose", problem: describe(e) })
    }
  }

  const pick = async (input: Input, d: Detected) => {
    if (d.importer.id !== "csv") {
      setState({ step: "provider", input, importer: d.importer })
      return
    }
    const file = tableFile(input)
    if (!file) return
    const table = await tableOf(file)
    if (!table.headers.length || !table.rows.length) {
      setState({
        step: "choose",
        problem: "It holds no rows under a header row.",
      })
      return
    }
    setState({ step: "csv", input, table })
  }

  switch (state.step) {
    case "choose":
      return (
        <ChooseInput
          importers={importers}
          onInput={(p) => void onInput(p)}
          problem={state.problem}
        />
      )
    case "reading":
      return (
        <div className="mb-7 flex flex-col items-center gap-3 py-10 text-[15px] text-secondary">
          <span className="flex items-center gap-2">
            <Spinner /> Reading what you chose…
          </span>
          <TextButton onClick={back}>Cancel</TextButton>
        </div>
      )
    case "detected":
      return (
        <ChooseImporter
          input={state.input}
          found={state.found}
          onPick={(d) => void pick(state.input, d)}
          onBack={back}
        />
      )
    case "csv":
      return (
        <CsvImport
          name={
            state.input.name || tableFile(state.input)?.path || "results.csv"
          }
          table={state.table}
          onBack={back}
        />
      )
    case "provider":
      return (
        <ProviderImport
          input={state.input}
          importer={state.importer}
          onBack={back}
        />
      )
  }
}
