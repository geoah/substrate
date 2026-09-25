/** Try it: a form built from a function's declared arguments that runs it
 * once, now, through the call API, and shows what it gave back. */

import { useMutation, useQueryClient } from "@tanstack/react-query"
import { CircleCheck, Play } from "lucide-react"
import { useId, useState, type FormEvent } from "react"

import { CodeBlock } from "@/components/code-block"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Textarea } from "@/components/ui/textarea"
import { callFunction } from "@/lib/api/functions"
import type { FunctionCalled } from "@/lib/api/types"
import {
  argumentLabel,
  buildCallInput,
  fieldControl,
  outputRows,
  sentence,
  type FieldValue,
  type ToolArgument,
} from "@/lib/tools"
import { cn } from "@/lib/utils"

/** More optional fields than this fold under "More options". */
const OPEN_OPTIONAL = 3

/** The one argument a function that declares none is asked for: its whole
 * input, as JSON. */
const OPEN_INPUT: ToolArgument = {
  name: "input",
  type: "json",
  repeated: false,
  required: false,
  description: "Whatever it takes, as JSON",
  values: [],
}

export function TryIt({
  tool,
  args,
  technical,
}: {
  /** The function reference. */
  tool: string
  args: ToolArgument[]
  technical: boolean
}) {
  const queryClient = useQueryClient()
  const [values, setValues] = useState<Record<string, FieldValue>>({})
  const [errors, setErrors] = useState<Record<string, string>>({})
  const [more, setMore] = useState(false)
  const open = args.length === 0
  const fields = open ? [OPEN_INPUT] : args
  const required = fields.filter((a) => a.required)
  const optional = fields.filter((a) => !a.required)
  const shown =
    more || optional.length <= OPEN_OPTIONAL
      ? optional
      : optional.slice(0, Math.max(0, OPEN_OPTIONAL - required.length))
  const hidden = optional.length - shown.length

  const run = useMutation({
    mutationFn: (input: unknown) => callFunction(tool, input),
    onSuccess: (res) => {
      // What it wrote shows up on the pages that list records.
      if (res.effects > 0)
        void queryClient.invalidateQueries({ queryKey: ["records"] })
    },
  })

  const submit = (e: FormEvent) => {
    e.preventDefault()
    const built = buildCallInput(fields, values)
    setErrors(built.errors)
    if (Object.keys(built.errors).length) return
    run.mutate(open ? (built.input.input ?? {}) : built.input)
  }

  const set = (name: string, value: FieldValue) =>
    setValues((v) => ({ ...v, [name]: value }))

  return (
    <div className="flex flex-col gap-3">
      <form
        onSubmit={submit}
        className="flex flex-col gap-3.5 rounded-[10px] border bg-panel p-3.5"
        aria-label="Try it"
      >
        {[...required, ...shown].map((a) => (
          <Field
            key={a.name}
            arg={a}
            value={values[a.name]}
            error={errors[a.name]}
            technical={technical}
            onChange={(v) => set(a.name, v)}
          />
        ))}
        {hidden > 0 && (
          <button
            type="button"
            onClick={() => setMore(true)}
            className="self-start text-[12.5px] text-primary-text hover:underline"
          >
            More options ({hidden})
          </button>
        )}
        <div className="flex items-center gap-3">
          <Button type="submit" size="sm" disabled={run.isPending}>
            <Play className="size-3.5" />
            {run.isPending ? "Running…" : "Run"}
          </Button>
          {run.isError && (
            <span className="text-[12.5px] text-destructive">
              It didn’t run. {(run.error as Error).message}
            </span>
          )}
        </div>
      </form>
      {run.data && <RunResult result={run.data} technical={technical} />}
    </div>
  )
}

function Field({
  arg,
  value,
  error,
  technical,
  onChange,
}: {
  arg: ToolArgument
  value: FieldValue | undefined
  error?: string
  technical: boolean
  onChange: (v: FieldValue) => void
}) {
  const id = useId()
  const control = fieldControl(arg)
  const text = typeof value === "string" ? value : ""
  const hint = control === "list" ? "One per line." : undefined
  const placeholder =
    control === "json"
      ? "{ }"
      : arg.description
        ? `${sentence(arg.description)}…`
        : undefined
  const invalid = Boolean(error)
  const label = (
    <label htmlFor={id} className="text-[12.5px] font-medium">
      {argumentLabel(arg.name)}
      {!arg.required && (
        <span className="ml-1.5 font-normal text-faint">optional</span>
      )}
      {technical && (
        <span className="ml-2 font-mono text-[11px] font-normal text-faint">
          {arg.name}
        </span>
      )}
    </label>
  )
  if (control === "checkbox") {
    return (
      <div className="flex items-start gap-2">
        <input
          id={id}
          type="checkbox"
          checked={value === true}
          onChange={(e) => onChange(e.target.checked)}
          className="mt-0.5 size-4 accent-primary"
        />
        <div className="flex flex-col gap-0.5">
          {label}
          {arg.description && (
            <span className="text-xs text-faint">
              {sentence(arg.description)}
            </span>
          )}
        </div>
      </div>
    )
  }
  return (
    <div className="flex flex-col gap-1.5">
      {label}
      {control === "select" ? (
        <select
          id={id}
          value={text}
          aria-invalid={invalid}
          onChange={(e) => onChange(e.target.value)}
          className="h-8 w-full rounded-md border border-input bg-background px-2.5 text-[13px]"
        >
          <option value="">{arg.required ? "Choose…" : "Not set"}</option>
          {arg.values.map((v) => (
            <option key={v} value={v}>
              {v}
            </option>
          ))}
        </select>
      ) : control === "textarea" || control === "json" || control === "list" ? (
        <Textarea
          id={id}
          value={text}
          aria-invalid={invalid}
          onChange={(e) => onChange(e.target.value)}
          placeholder={placeholder}
          className={cn(
            "min-h-16 bg-background text-[13px]",
            control === "json" && "font-mono text-xs"
          )}
        />
      ) : (
        <Input
          id={id}
          value={text}
          inputMode={control === "number" ? "decimal" : undefined}
          placeholder={placeholder}
          aria-invalid={invalid}
          onChange={(e) => onChange(e.target.value)}
          className="h-8 bg-background text-[13px]"
        />
      )}
      {error ? (
        <span className="text-xs text-destructive">{error}</span>
      ) : (
        hint && <span className="text-xs text-faint">{hint}</span>
      )}
    </div>
  )
}

function RunResult({
  result,
  technical,
}: {
  result: FunctionCalled
  technical: boolean
}) {
  const rows = outputRows(result.output)
  const changed =
    result.effects > 0
      ? `It made ${result.effects === 1 ? "1 change" : `${result.effects} changes`}.`
      : "It changed nothing."
  return (
    <div
      role="status"
      className="flex flex-col gap-2.5 rounded-[10px] border p-3.5"
    >
      <div className="flex items-center gap-2 text-[13px]">
        <CircleCheck className="size-4 text-ok" />
        <span className="font-medium">Done.</span>
        <span className="text-muted-foreground">{changed}</span>
      </div>
      {technical ? (
        <CodeBlock
          lang="json"
          source={JSON.stringify(result.output ?? null, null, 2)}
          className="max-h-96 overflow-auto rounded-[8px] border bg-background px-3 py-2"
        />
      ) : rows.length ? (
        <dl className="grid grid-cols-[minmax(0,160px)_minmax(0,1fr)] gap-x-3 gap-y-1.5 text-[13px]">
          {rows.map((r) => (
            <div key={r.label} className="contents">
              <dt className="text-muted-foreground">{r.label}</dt>
              <dd className="break-words">{r.value}</dd>
            </div>
          ))}
        </dl>
      ) : null}
    </div>
  )
}
