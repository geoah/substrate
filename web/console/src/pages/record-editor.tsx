/** The record editor (create + edit a record of any kind), in TWO LENSES over
 * ONE document.
 *
 * - **Form** is the default: one typed control per declared property, composed
 *   from the declaration (`PropertyForm`). An enum is a select, a state offers
 *   its states, a reference picks a record, a secret is write-only, and every
 *   control carries the property's one-liner and a worked example.
 * - **YAML** is the expert lens: the whole apply-able envelope in a code editor
 *   that knows the kind (`YamlEditor`, CodeMirror). Completion, diagnostics and
 *   hovers all read the declaration.
 *
 * The YAML text is the truth for both: the form reads its values out of the
 * document and writes them back one key at a time, so switching lenses is
 * lossless and a hand-authored comment survives being edited on the form.
 *
 * - **New** (`/data/:authority/:package/:kind/new`) seeds a schema-derived
 *   template (`templateYAML`); **Edit**
 *   (`/data/:authority/:package/:kind/:id/edit`) seeds the
 *   record's apply-able YAML (`applyManifestYAML`, the manifest view's shape
 *   minus server-owned `status`).
 * - Validation runs as the owner types (`validateApplyDoc`): the document
 *   parses, required properties are present, every value satisfies its
 *   DATATYPE, a state is not being moved by a put, and the id is the one the
 *   write lands on. Problems key to lines, the gutter marks them, and Save is
 *   barred while any error stands.
 * - Save applies through the record write API (POST for new, PUT upsert for
 *   edit). A server rejection is parsed and shown inline; the flow never claims
 *   success unless the apply returns ok. */

import { Suspense, lazy, useEffect, useMemo, useState } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Link, useNavigate } from "@tanstack/react-router"
import {
  AlertCircleIcon,
  AlertTriangleIcon,
  CheckCircle2Icon,
  FileQuestionIcon,
  WandSparklesIcon,
} from "lucide-react"

import { PropertyForm } from "@/components/record/property-form"

/** CodeMirror and its YAML grammar are the editor's alone: they load when the
 * YAML lens is first opened, never with the rest of the console (the same
 * discipline the shiki chunk keeps). */
const YamlEditor = lazy(() =>
  import("@/components/record/yaml-editor").then((m) => ({
    default: m.YamlEditor,
  }))
)
import { Button } from "@/components/ui/button"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Skeleton } from "@/components/ui/skeleton"
import { Spinner } from "@/components/ui/spinner"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { toast } from "@/components/ui/toast"
import { createRecord, recordQueryOptions, putRecord } from "@/lib/api/records"
import { kindsQueryOptions } from "@/lib/api/kinds"
import { ApiError, type SubstrateRecord, type KindInfo } from "@/lib/api/types"
import { AGENT_KIND, grantHints } from "@/lib/agent-grants"
import {
  applyManifestYAML,
  formatYAML,
  parseApplyDoc,
  propertiesOf,
  templateYAML,
  toPutInput,
  validateApplyDoc,
  type Problem,
} from "@/lib/record-yaml"
import { kindByCollection } from "@/lib/definition"
import { cn } from "@/lib/utils"
import { recordEditRoute, recordNewRoute } from "@/router"

type Mode = "create" | "edit"
type Lens = "form" | "yaml"

/** New-record route wrapper (`/data/:authority/:package/:kind/new`). */
export function RecordNewPage() {
  const { authority, pkg, name: plural } = recordNewRoute.useParams()
  return (
    <RecordEditor
      authority={authority}
      pkg={pkg}
      plural={plural}
      mode="create"
    />
  )
}

/** Edit route wrapper (`/data/:authority/:package/:kind/:id/edit`). */
export function RecordEditPage() {
  const { authority, pkg, name: plural, id } = recordEditRoute.useParams()
  return (
    <RecordEditor
      authority={authority}
      pkg={pkg}
      plural={plural}
      mode="edit"
      id={id}
    />
  )
}

function RecordEditor({
  authority,
  pkg,
  plural,
  mode,
  id,
}: {
  authority: string
  pkg: string
  plural: string
  mode: Mode
  id?: string
}) {
  const registry = useQuery(kindsQueryOptions)
  const kindInfo = registry.data
    ? kindByCollection(registry.data, authority, pkg, plural)
    : undefined
  const record = useQuery({
    ...recordQueryOptions(authority, pkg, plural, id ?? ""),
    enabled: mode === "edit" && Boolean(id),
  })

  const ready = !registry.isPending && (mode === "create" || !record.isPending)

  if (!ready) return <EditorSkeleton />

  if (registry.isError || !kindInfo) {
    return (
      <EditorEmpty
        title="Unknown collection"
        description={`${authority}/${plural} is not in the kind registry.`}
      />
    )
  }
  if (mode === "edit" && record.isError) {
    return (
      <EditorEmpty
        title="The record didn't load"
        description={`${authority}/${plural}/${id} — ${record.error.message}`}
      />
    )
  }

  const seed =
    mode === "edit" && record.data
      ? applyManifestYAML(record.data, kindInfo)
      : templateYAML(kindInfo)

  return (
    <RecordEditorForm
      authority={authority}
      pkg={pkg}
      plural={plural}
      mode={mode}
      kind={kindInfo}
      kinds={registry.data ?? []}
      record={mode === "edit" ? record.data : undefined}
      seed={seed}
    />
  )
}

/** The editor proper, over plain props (no route, so it is directly testable). */
export function RecordEditorForm({
  authority,
  pkg,
  plural,
  mode,
  kind,
  kinds,
  record,
  seed,
}: {
  authority: string
  pkg: string
  plural: string
  mode: Mode
  kind: KindInfo
  kinds: KindInfo[]
  record?: SubstrateRecord
  seed: string
}) {
  const navigate = useNavigate()
  const queryClient = useQueryClient()

  const [text, setText] = useState(seed)
  // Reseed if the underlying seed changes (e.g. an edit's record finished
  // loading, or the version bumped) — but never clobber in-flight edits after
  // the first mount, so key the reseed to the seed value itself.
  const [seededFrom, setSeededFrom] = useState(seed)
  if (seededFrom !== seed) {
    setSeededFrom(seed)
    setText(seed)
  }

  // The lens is this editor's own state, not the URL's: the address already
  // names the record, and a half-typed document is nobody's link.
  const [lens, setLens] = useState<Lens>("form")

  // The API's own rejection (schema/admission), shown inline until the next edit.
  const [serverError, setServerError] = useState<ApiError | undefined>()

  // Debounced text feeds the problems panel so we don't re-validate the whole
  // document on every keystroke.
  const [debounced, setDebounced] = useState(text)
  useEffect(() => {
    const t = setTimeout(() => setDebounced(text), 250)
    return () => clearTimeout(t)
  }, [text])

  const ctx = useMemo(() => ({ record }), [record])

  // The document's own problems, plus the grants an agent's host tools need:
  // the loader refuses an unpaid tool, so the panel says which property pays
  // for it rather than letting the apply be the first place it is mentioned.
  const problems = useMemo(() => {
    const found = validateApplyDoc(debounced, kind, ctx)
    if (kind.identity !== AGENT_KIND) return found
    const hints: Problem[] = grantHints(propertiesOf(debounced)).map(
      (hint) => ({
        severity: "warning" as const,
        message: hint.message,
        path: hint.property,
      })
    )
    return [...found, ...hints]
  }, [debounced, kind, ctx])
  // Save is gated on the LIVE text, not the debounced copy — a fast Save right
  // after a fix must see the fix.
  const liveErrors = useMemo(
    () =>
      validateApplyDoc(text, kind, ctx).filter((p) => p.severity === "error"),
    [text, kind, ctx]
  )
  const errorCount = liveErrors.length
  const warnCount = problems.filter((p) => p.severity === "warning").length

  const mutation = useMutation({
    mutationFn: async () => {
      const parsed = parseApplyDoc(text)
      if (parsed.error || !parsed.value) {
        throw new ApiError(
          "validation",
          parsed.error?.message ?? "The document did not parse.",
          0
        )
      }
      const input = toPutInput(parsed.value, kind)
      return mode === "edit" && record
        ? putRecord(authority, pkg, plural, record.id, input)
        : createRecord(authority, pkg, plural, input)
    },
    onSuccess: (saved) => {
      toast.add({
        type: "success",
        title:
          mode === "edit" ? `${kind.name} updated.` : `${kind.name} created.`,
      })
      void queryClient.invalidateQueries()
      void navigate({
        to: "/data/$authority/$pkg/$name/$id",
        params: {
          authority: authority,
          pkg: pkg,
          name: plural,
          id: saved.id,
        },
      })
    },
    onError: (error) => {
      const api =
        error instanceof ApiError
          ? error
          : new ApiError("network", (error as Error).message, 0)
      setServerError(api)
      toast.add({
        type: "error",
        title:
          mode === "edit"
            ? `Could not update the ${kind.name}`
            : `Could not create the ${kind.name}`,
        description: api.message,
      })
    },
  })

  function onChange(next: string) {
    setText(next)
    if (serverError) setServerError(undefined)
  }

  function format() {
    const { text: formatted, error } = formatYAML(text)
    if (error) {
      toast.add({
        type: "error",
        title: "Nothing to format",
        description: error,
      })
      return
    }
    onChange(formatted)
  }

  const canSave = errorCount === 0 && !mutation.isPending

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex shrink-0 items-start justify-between gap-3 px-6 pt-5 pb-3">
        <div className="min-w-0">
          <h1 className="truncate text-lg font-semibold">
            {mode === "edit" ? `Edit ${kind.name}` : `New ${kind.name}`}
          </h1>
          <p className="data text-xs text-muted-foreground">
            {mode === "edit" && record
              ? `${authority}/${plural}/${record.id}`
              : `${authority}/${plural}`}
          </p>
        </div>
        <div className="flex shrink-0 items-center gap-1.5 pt-0.5">
          {lens === "yaml" && (
            <Button
              variant="ghost"
              size="sm"
              className="gap-1.5"
              disabled={mutation.isPending}
              onClick={format}
            >
              <WandSparklesIcon className="size-3.5" />
              Format
            </Button>
          )}
          <Button
            variant="outline"
            size="sm"
            disabled={mutation.isPending}
            render={
              mode === "edit" && record ? (
                <Link
                  to="/data/$authority/$pkg/$name/$id"
                  params={{
                    authority: authority,
                    pkg: pkg,
                    name: plural,
                    id: record.id,
                  }}
                />
              ) : (
                <Link
                  to="/data/$authority/$pkg/$name"
                  params={{ authority: authority, pkg: pkg, name: plural }}
                />
              )
            }
          >
            Cancel
          </Button>
          <Button
            size="sm"
            className="gap-1.5"
            disabled={!canSave}
            onClick={() => mutation.mutate()}
          >
            {mutation.isPending && <Spinner className="size-3.5" />}
            {mode === "edit" ? "Save changes" : "Create"}
          </Button>
        </div>
      </div>

      <div className="flex min-h-0 flex-1 flex-col border-t xl:flex-row">
        <Tabs
          value={lens}
          onValueChange={(next) => setLens(next as Lens)}
          className="min-h-0 flex-1 gap-0"
        >
          <TabsList variant="line" className="mx-4 mt-2 shrink-0 justify-start">
            <TabsTrigger value="form">Form</TabsTrigger>
            <TabsTrigger value="yaml">YAML</TabsTrigger>
          </TabsList>

          <TabsContent value="form" className="min-h-0 border-t">
            <ScrollArea className="h-full">
              <PropertyForm
                text={text}
                kind={kind}
                kinds={kinds}
                record={record}
                onChange={onChange}
              />
            </ScrollArea>
          </TabsContent>
          <TabsContent value="yaml" className="min-h-0 border-t">
            <Suspense
              fallback={
                <div className="p-4">
                  <Skeleton className="h-4 w-64" />
                </div>
              }
            >
              <YamlEditor
                value={text}
                onChange={onChange}
                kind={kind}
                ctx={ctx}
              />
            </Suspense>
          </TabsContent>
        </Tabs>

        <div className="shrink-0 border-t xl:w-[24rem] xl:border-t-0 xl:border-l">
          <ProblemsPanel
            problems={problems}
            errorCount={errorCount}
            warnCount={warnCount}
            serverError={serverError}
            onShowLine={() => setLens("yaml")}
          />
        </div>
      </div>
    </div>
  )
}

/** The validation surface: a status line, the API's rejection when there is
 * one, and the live client-side problems, each keyed to its line. */
function ProblemsPanel({
  problems,
  errorCount,
  warnCount,
  serverError,
  onShowLine,
}: {
  problems: Problem[]
  errorCount: number
  warnCount: number
  serverError?: ApiError
  onShowLine: () => void
}) {
  const clean = errorCount === 0 && warnCount === 0 && !serverError
  return (
    <ScrollArea className="h-full">
      <div className="flex flex-col gap-3 p-4">
        <div className="flex items-center gap-2 text-sm font-medium">
          {clean ? (
            <>
              <CheckCircle2Icon className="size-4 text-primary" />
              <span>Ready to apply</span>
            </>
          ) : (
            <span>
              {errorCount > 0 && (
                <span className="text-destructive">
                  {errorCount} {errorCount === 1 ? "error" : "errors"}
                </span>
              )}
              {errorCount > 0 && warnCount > 0 && ", "}
              {warnCount > 0 && (
                <span className="text-muted-foreground">
                  {warnCount} {warnCount === 1 ? "warning" : "warnings"}
                </span>
              )}
            </span>
          )}
        </div>

        {serverError && (
          <div className="rounded-md border border-destructive/40 bg-destructive/5 p-3">
            <div className="flex items-center gap-2 text-sm font-medium text-destructive">
              <AlertCircleIcon className="size-4 shrink-0" />
              The substrate rejected this apply
            </div>
            <p className="mt-1 text-xs text-muted-foreground">
              {serverError.message}
            </p>
            {serverError.problems.length > 0 && (
              <ul className="mt-2 flex flex-col gap-1">
                {serverError.problems.map((p, i) => (
                  <li key={i} className="data text-xs text-muted-foreground">
                    {p}
                  </li>
                ))}
              </ul>
            )}
          </div>
        )}

        {clean ? (
          <p className="text-xs text-muted-foreground">
            The document parses and satisfies the kind's schema.
          </p>
        ) : (
          <ul className="flex flex-col gap-2">
            {problems.map((p, i) => (
              <ProblemRow key={i} problem={p} onShowLine={onShowLine} />
            ))}
          </ul>
        )}
      </div>
    </ScrollArea>
  )
}

function ProblemRow({
  problem,
  onShowLine,
}: {
  problem: Problem
  onShowLine: () => void
}) {
  const isError = problem.severity === "error"
  return (
    <li className="flex items-start gap-2 text-xs">
      {isError ? (
        <AlertCircleIcon className="mt-0.5 size-3.5 shrink-0 text-destructive" />
      ) : (
        <AlertTriangleIcon className="mt-0.5 size-3.5 shrink-0 text-warning" />
      )}
      <div className="min-w-0">
        <span
          className={cn(isError ? "text-foreground" : "text-muted-foreground")}
        >
          {problem.message}
        </span>
        {problem.line !== undefined && (
          <button
            type="button"
            onClick={onShowLine}
            className="ml-1.5 data text-muted-foreground underline-offset-4 hover:underline"
          >
            line {problem.line}
          </button>
        )}
      </div>
    </li>
  )
}

function EditorEmpty({
  title,
  description,
}: {
  title: string
  description: string
}) {
  return (
    <div className="flex flex-1 p-6">
      <Empty>
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <FileQuestionIcon />
          </EmptyMedia>
          <EmptyTitle>{title}</EmptyTitle>
          <EmptyDescription>
            <span className="data">{description}</span>
          </EmptyDescription>
        </EmptyHeader>
      </Empty>
    </div>
  )
}

function EditorSkeleton() {
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="shrink-0 px-6 pt-5 pb-3">
        <Skeleton className="h-6 w-40" />
        <Skeleton className="mt-1.5 h-3.5 w-56" />
      </div>
      <div className="flex flex-1 flex-col gap-2 border-t px-6 pt-4">
        {Array.from({ length: 10 }, (_, i) => (
          <Skeleton
            key={i}
            className="h-3.5"
            style={{ width: `${[40, 55, 30, 65, 45, 50, 35, 60, 42, 48][i]}%` }}
          />
        ))}
      </div>
    </div>
  )
}
