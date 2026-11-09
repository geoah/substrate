import { Confirm, cx } from "@substrate/react"
import { ChevronRight } from "lucide-react"
import { useState } from "react"
import { useBackend } from "../../data"
import type { ImportRecord } from "../../kinds"
import { ownImporter, resumable } from "../../import/runner"
import { formatDate } from "../../lib/format"
import { routeHash } from "../../route"
import { Badge } from "../Badge"
import { TextButton } from "../IconButton"
import {
  IMPORTER_WORDS,
  STATUS,
  countsWords,
  createdWords,
  runWords,
} from "../import/words"

/** The actions an import offers: Resume while it can be, Undo until it is
 * undone (and again while anything still points at an undone one); for a
 * staging one, Finish in the browser that keeps its files, and Discard
 * from any. */
export function ImportActionsBar(props: { imp: ImportRecord }) {
  const { imp } = props
  const sources = useBackend().sources
  const actions = sources.useImportActions()
  const run = sources.useRunState(imp.id)
  const created = sources.useCreated(imp).data ?? 0
  const kept = sources.useKept().has(imp.id)
  const [asking, setAsking] = useState<"undo" | "discard" | null>(null)
  const staging = imp.status === "staging"
  const canFinish = staging && kept && !run.running
  const canDiscard = staging && !(run.running && run.mode === "discard")
  const canResume = resumable(imp.status) && !run.running
  const canUndo =
    !staging &&
    (imp.status !== "undone" || created > 0) &&
    !(run.running && run.mode === "undo")
  const undoLabel = imp.status === "undone" ? "Undo Again" : "Undo"
  const own = ownImporter(imp.importer)
  if (!canFinish && !canDiscard && !canResume && !canUndo) return null
  return (
    <>
      <div className="flex flex-wrap gap-x-5">
        {canFinish && (
          <TextButton
            className="font-semibold"
            onClick={() => void actions.finish(imp.id)}
          >
            Finish
          </TextButton>
        )}
        {canResume && (
          <TextButton
            className="font-semibold"
            onClick={() => void actions.resume(imp.id)}
          >
            {imp.status === "planned" ? "Start" : "Resume"}
          </TextButton>
        )}
        {canDiscard && (
          <TextButton
            className="text-danger"
            onClick={() => setAsking("discard")}
          >
            Discard
          </TextButton>
        )}
        {canUndo && (
          <TextButton className="text-danger" onClick={() => setAsking("undo")}>
            {undoLabel}
          </TextButton>
        )}
      </div>
      <Confirm
        open={asking === "undo"}
        title={imp.status === "undone" ? "Undo again?" : "Undo this import?"}
        message={
          imp.status === "undone"
            ? `${createdWords(created)} still point at this import, written after it was undone. They are deleted from your repository.`
            : own
              ? "Every report and result it wrote is deleted from your repository. A write still in flight is waited for first."
              : "Every record it created is deleted from your repository. What it only updated stays as it updated it, and a source or a care provider is never deleted. A write still in flight is waited for first."
        }
        confirm={undoLabel}
        onCancel={() => setAsking(null)}
        onConfirm={() => {
          setAsking(null)
          void actions.undo(imp.id)
        }}
      />
      <Confirm
        open={asking === "discard"}
        title="Discard this import?"
        message={
          kept
            ? "It is deleted before anything of it is written, and the files this browser keeps for it are dropped."
            : "It is deleted before anything of it is written. The browser that is staging it stops when it next tries."
        }
        confirm="Discard"
        onCancel={() => setAsking(null)}
        onConfirm={() => {
          setAsking(null)
          void actions.discard(imp.id)
        }}
      />
    </>
  )
}

/** One import on Sources or a source's page: its name, its importer and
 * state, the latest run's counts and what it created, its problem or its
 * run, and its actions. */
export function ImportItem(props: { imp: ImportRecord; sourceName?: string }) {
  const { imp } = props
  const sources = useBackend().sources
  const run = sources.useRunState(imp.id)
  const created = sources.useCreated(imp).data
  const kept = sources.useKept().has(imp.id)
  const status = STATUS[imp.status]
  const words = runWords(run)
  const counts = imp.status === "staging" ? undefined : countsWords(imp.counts)
  const when = imp.importedAt
  return (
    <div className="px-4 py-2.5">
      <a
        href={routeHash({ screen: "import", id: imp.id })}
        className="flex min-h-11 items-start gap-3 active:opacity-70"
      >
        <span className="flex min-w-0 flex-1 flex-col gap-0.5">
          <span className="text-[17px] break-words">{imp.name}</span>
          <span className="text-[15px] break-words text-secondary">
            {[
              imp.importer && IMPORTER_WORDS[imp.importer] !== imp.name
                ? IMPORTER_WORDS[imp.importer]
                : undefined,
              props.sourceName,
              when ? formatDate(when) : undefined,
            ]
              .filter(Boolean)
              .join(", ")}
          </span>
          {counts && (
            <span className="text-[15px] break-words text-secondary">
              {`Latest run: ${counts}`}
            </span>
          )}
          {created !== undefined && imp.status !== "staging" && (
            <span className="text-[15px] text-secondary">
              {imp.status === "undone"
                ? created > 0
                  ? `${createdWords(created)} still point at it, written after it was undone.`
                  : "Nothing of it is left."
                : `It created ${createdWords(created)}.`}
            </span>
          )}
        </span>
        <Badge label={status.word} tone={status.tone} className="mt-0.5" />
        <ChevronRight
          className="mt-1 size-4 shrink-0 text-tertiary"
          aria-hidden
        />
      </a>
      {imp.status === "staging" && !run.running && (
        <p className="text-[15px] break-words text-secondary">
          {kept
            ? "This page closed before it finished sending the files. Finish sends the rest from this browser."
            : "Being staged by the browser that read its files. Only that browser can finish it."}
        </p>
      )}
      {imp.status === "stopped" && imp.problem && (
        <p className="text-warn-ink text-[15px] break-words">{imp.problem}</p>
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
