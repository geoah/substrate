import { Confirm, cx } from "@substrate/react"
import { ChevronRight } from "lucide-react"
import { useState } from "react"
import { useBackend } from "../../data"
import type { LabImport } from "../../kinds"
import { resumable } from "../../import/runner"
import { formatDate } from "../../lib/format"
import { routeHash } from "../../route"
import { Badge } from "../Badge"
import { TextButton } from "../IconButton"
import { STATUS, countWords, runWords } from "../import/words"

/** The actions an import offers: Resume while it can be, Undo until it is
 * undone, and Undo again while anything still points at an undone one. */
export function ImportActionsBar(props: { imp: LabImport }) {
  const { imp } = props
  const sources = useBackend().sources
  const actions = sources.useImportActions()
  const run = sources.useRunState(imp.id)
  const still = sources.useStillPointing(imp.id, imp.status === "undone")
  const [asking, setAsking] = useState(false)
  const left = still.data ?? 0
  const canResume = resumable(imp.status) && !run.running
  const canUndo =
    (imp.status !== "undone" || left > 0) &&
    !(run.running && run.mode === "undo")
  const undoLabel = imp.status === "undone" ? "Undo again" : "Undo"
  return (
    <>
      {(canResume || canUndo) && (
        <div className="flex flex-wrap gap-x-5">
          {canResume && (
            <TextButton
              className="font-semibold"
              onClick={() => void actions.resume(imp.id)}
            >
              {imp.status === "planned" ? "Start" : "Resume"}
            </TextButton>
          )}
          {canUndo && (
            <TextButton className="text-danger" onClick={() => setAsking(true)}>
              {undoLabel}
            </TextButton>
          )}
        </div>
      )}
      <Confirm
        open={asking}
        title={imp.status === "undone" ? "Undo again?" : "Undo this import?"}
        message={
          imp.status === "undone"
            ? `${left.toLocaleString("en-GB")} ${left === 1 ? "record still points" : "records still point"} at this import, written after it was undone. They are deleted from your repository.`
            : "Every report and result it wrote is deleted from your repository. A write still in flight is waited for first."
        }
        confirm={undoLabel}
        onCancel={() => setAsking(false)}
        onConfirm={() => {
          setAsking(false)
          void actions.undo(imp.id)
        }}
      />
    </>
  )
}

/** One import on Sources or a source's page: its name, its state, its
 * counts, its problem or its run, Resume and Undo. */
export function ImportItem(props: { imp: LabImport; sourceName?: string }) {
  const { imp } = props
  const sources = useBackend().sources
  const run = sources.useRunState(imp.id)
  const still = sources.useStillPointing(imp.id, imp.status === "undone")
  const status = STATUS[imp.status]
  const words = runWords(run)
  const left = still.data ?? 0
  const when = imp.importedAt ?? imp.reports[0]?.takenAt
  return (
    <div className="px-4 py-2.5">
      <a
        href={routeHash({ screen: "import", id: imp.id })}
        className="flex min-h-11 items-start gap-3 active:opacity-70"
      >
        <span className="flex min-w-0 flex-1 flex-col gap-0.5">
          <span className="text-[17px] break-words">{imp.name}</span>
          <span className="text-[15px] text-secondary">
            {[
              countWords(
                imp.results,
                imp.rows.length,
                imp.status !== "importing" &&
                  imp.status !== "stopped" &&
                  imp.status !== "undoing"
              ),
              props.sourceName,
              when ? formatDate(when) : undefined,
            ]
              .filter(Boolean)
              .join(", ")}
          </span>
        </span>
        <Badge label={status.word} tone={status.tone} className="mt-0.5" />
        <ChevronRight
          className="mt-1 size-4 shrink-0 text-tertiary"
          aria-hidden
        />
      </a>
      {imp.status === "stopped" && imp.problem && (
        <p className="text-warn-ink text-[15px] break-words">{imp.problem}</p>
      )}
      {imp.status === "undone" && left > 0 && (
        <p className="text-warn-ink text-[15px]">
          {left.toLocaleString("en-GB")}{" "}
          {left === 1 ? "record still points" : "records still point"} at it,
          written after it was undone.
        </p>
      )}
      {words && (
        <p
          role="status"
          className={cx(
            "text-[15px] break-words",
            run.running ? "text-info-ink" : "text-secondary"
          )}
        >
          {words}
        </p>
      )}
      <ImportActionsBar imp={imp} />
    </div>
  )
}
