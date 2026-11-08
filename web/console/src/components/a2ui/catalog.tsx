/** The A2UI v1.0 basic catalog drawn with the console's own components, so
 * an applet looks like the console in both themes. Each component reads its
 * props through the surface's dynamic values; the inputs write their bound
 * path in the local data model, and only a button's event reaches the
 * server. Prop names, enums and defaults are the catalog's; surface.tsx maps
 * each catalog name to its component here. */

import {
  createElement,
  useContext,
  useEffect,
  useId,
  useState,
  type KeyboardEvent,
  type ReactNode,
} from "react"

import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Separator } from "@/components/ui/separator"
import { Spinner } from "@/components/ui/spinner"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Textarea } from "@/components/ui/textarea"
import { failingChecks } from "@/lib/a2ui/checks"
import { isRecord, toNumber, toStringList, toText } from "@/lib/a2ui/coerce"
import { childRef, childRefs } from "@/lib/a2ui/tree"
import { mediaUrl } from "@/lib/a2ui/urls"
import { proxied, proxiedImage } from "@/lib/api/images"
import {
  bindingPath,
  resolve,
  resolveText,
  type EvalContext,
} from "@/lib/a2ui/values"
import { cn } from "@/lib/utils"

import {
  PhrasingContext,
  useSurface,
  type NodeProps,
  type RootProps,
} from "./context"
import { iconByName } from "./icons"
import { MarkdownText } from "./markdown-text"
import { Problem } from "./problem"

/** The root's layout props on the wrapper, its accessibility on the
 * control a reader operates. */
function split(root: RootProps) {
  const { style, "data-a2ui": tag, ...aria } = root
  return { frame: { style, "data-a2ui": tag }, aria }
}

function Children({
  list,
  ctx,
  depth,
}: {
  list: unknown
  ctx: EvalContext
  depth: number
}) {
  const env = useSurface()
  return childRefs(list, ctx).map((ref) => env.child(ref, depth + 1))
}

function Child({
  id,
  ctx,
  depth,
}: {
  id: unknown
  ctx: EvalContext
  depth: number
}) {
  const env = useSurface()
  const ref = childRef(id, ctx)
  return ref ? env.child(ref, depth + 1) : null
}

// ── display ─────────────────────────────────────────────────────────────────

export function Text({ def, ctx, root }: NodeProps) {
  const text = resolveText(def.text, ctx)
  const phrasing = useContext(PhrasingContext)
  const Tag = phrasing ? "span" : "div"
  return (
    <Tag
      {...root}
      className={cn(
        "block min-w-0 space-y-1.5 break-words",
        def.variant === "caption"
          ? "text-xs text-muted-foreground"
          : "text-sm text-foreground"
      )}
    >
      <MarkdownText text={text} inline={phrasing} />
    </Tag>
  )
}

/** Each variant's box; `pending` is the height a box sized by its picture
 * holds while that picture is fetched. */
const IMAGE_VARIANTS: Record<
  string,
  { box: string; fit: string; pending?: string }
> = {
  icon: { box: "size-6 rounded", fit: "cover" },
  avatar: { box: "size-10 rounded-full", fit: "cover" },
  smallFeature: { box: "size-20 rounded-md", fit: "cover" },
  mediumFeature: {
    box: "h-auto w-full max-w-sm rounded-lg",
    fit: "fill",
    pending: "aspect-[4/3]",
  },
  largeFeature: {
    box: "h-auto max-h-96 w-full rounded-lg",
    fit: "fill",
    pending: "aspect-video",
  },
  header: { box: "h-40 w-full rounded-lg sm:h-56", fit: "cover" },
}

const FITS: Record<string, string> = {
  contain: "object-contain",
  cover: "object-cover",
  fill: "object-fill",
  none: "object-none",
  scaleDown: "object-scale-down",
}

export function Img({ def, ctx, root }: NodeProps) {
  const url = mediaUrl(resolve(def.url, ctx), { data: true })
  const src = useImageSource(url)
  const [broken, setBroken] = useState<string>()
  if (!url || src === null || broken === url) return null
  const variant =
    IMAGE_VARIANTS[typeof def.variant === "string" ? def.variant : ""] ??
    IMAGE_VARIANTS.mediumFeature
  // The box is held while the picture is fetched, so a list of rows does
  // not shift as each one lands.
  if (src === undefined)
    return (
      <span
        {...root}
        aria-hidden
        data-slot="a2ui-image-pending"
        className={cn("block shrink-0 bg-muted", variant.box, variant.pending)}
      />
    )
  const fit = FITS[typeof def.fit === "string" ? def.fit : variant.fit]
  return (
    <img
      {...root}
      src={src}
      alt={resolveText(def.description, ctx)}
      loading="lazy"
      decoding="async"
      referrerPolicy="no-referrer"
      onError={() => setBroken(url)}
      className={cn("shrink-0 bg-muted", variant.box, fit)}
    />
  )
}

/** The source an image is drawn from: the URL itself on this origin or
 * inline, the proxy's blob URL for one hosted elsewhere; undefined while
 * that is fetched, null when there is nothing to draw. */
function useImageSource(url: string | undefined): string | null | undefined {
  const [fetched, setFetched] = useState<{ url: string; src?: string }>()
  const remote = url !== undefined && proxied(url)
  useEffect(() => {
    if (!url || !remote) return
    let live = true
    void proxiedImage(url).then((src) => {
      if (live) setFetched({ url, src })
    })
    return () => {
      live = false
    }
  }, [url, remote])
  if (!url) return null
  if (!remote) return url
  return fetched?.url === url ? (fetched.src ?? null) : undefined
}

export function Icon({ def, ctx, root }: NodeProps) {
  const name = resolve(def.name, ctx)
  const labelled = Boolean(root["aria-label"])
  if (isRecord(name) && "svgPath" in name) {
    return (
      <svg
        {...root}
        viewBox="0 0 24 24"
        fill="currentColor"
        role={labelled ? "img" : undefined}
        aria-hidden={labelled ? undefined : true}
        className="size-5 shrink-0"
      >
        <path d={resolveText(name.svgPath, ctx)} />
      </svg>
    )
  }
  const lucide = typeof name === "string" ? iconByName(name) : undefined
  if (!lucide)
    return <Problem message={`${def.id}: no icon named “${toText(name)}”`} />
  return createElement(lucide, {
    ...root,
    role: labelled ? "img" : undefined,
    "aria-hidden": labelled ? undefined : true,
    className: "size-5 shrink-0",
    strokeWidth: 1.8,
  })
}

export function Video({ def, ctx, root }: NodeProps) {
  const url = mediaUrl(resolve(def.url, ctx))
  if (!url) return null
  return (
    <video
      {...root}
      src={url}
      poster={mediaUrl(resolve(def.posterUrl, ctx), { data: true })}
      controls
      playsInline
      preload="metadata"
      className="max-h-96 w-full rounded-lg bg-black"
    />
  )
}

export function AudioPlayer({ def, ctx, root }: NodeProps) {
  const url = mediaUrl(resolve(def.url, ctx))
  if (!url) return null
  const description = resolveText(def.description, ctx)
  const { frame, aria } = split(root)
  return (
    <figure {...frame} className="flex min-w-0 flex-col gap-1.5">
      <audio
        {...aria}
        src={url}
        controls
        preload="metadata"
        className="w-full"
      />
      {description && (
        <figcaption className="text-xs text-muted-foreground">
          {description}
        </figcaption>
      )}
    </figure>
  )
}

// ── layout ──────────────────────────────────────────────────────────────────

const JUSTIFY: Record<string, string> = {
  start: "justify-start",
  center: "justify-center",
  end: "justify-end",
  spaceBetween: "justify-between",
  spaceAround: "justify-around",
  spaceEvenly: "justify-evenly",
  stretch: "justify-stretch",
}

const ALIGN: Record<string, string> = {
  start: "items-start",
  center: "items-center",
  end: "items-end",
  stretch: "items-stretch",
}

function pick(map: Record<string, string>, value: unknown, fallback: string) {
  return map[typeof value === "string" ? value : ""] ?? map[fallback]
}

export function Row({ def, ctx, depth, root }: NodeProps) {
  const Tag = useContext(PhrasingContext) ? "span" : "div"
  return (
    <Tag
      {...root}
      className={cn(
        // a2ui-row marks the children a borderless button sizes to.
        "a2ui-row flex min-w-0 flex-row flex-wrap gap-x-3 gap-y-2",
        pick(JUSTIFY, def.justify, "start"),
        pick(ALIGN, def.align, "stretch")
      )}
    >
      <Children list={def.children} ctx={ctx} depth={depth} />
    </Tag>
  )
}

export function Column({ def, ctx, depth, root }: NodeProps) {
  const Tag = useContext(PhrasingContext) ? "span" : "div"
  return (
    <Tag
      {...root}
      className={cn(
        "flex min-w-0 flex-col gap-3",
        pick(JUSTIFY, def.justify, "start"),
        pick(ALIGN, def.align, "stretch")
      )}
    >
      <Children list={def.children} ctx={ctx} depth={depth} />
    </Tag>
  )
}

export function List({ def, ctx, depth, root }: NodeProps) {
  const env = useSurface()
  const horizontal = def.direction === "horizontal"
  return (
    <div
      {...root}
      role="list"
      className={cn(
        "flex min-w-0",
        horizontal
          ? "snap-x flex-row gap-3 overflow-x-auto pb-1 [&>*]:shrink-0 [&>*]:snap-start"
          : "flex-col gap-0.5",
        pick(ALIGN, def.align, "stretch")
      )}
    >
      {childRefs(def.children, ctx).map((ref) => (
        <div key={ref.key} role="listitem" className="flex min-w-0 flex-col">
          {env.child({ ...ref, key: "item" }, depth + 1)}
        </div>
      ))}
    </div>
  )
}

export function Card({ def, ctx, depth, root }: NodeProps) {
  const Tag = useContext(PhrasingContext) ? "span" : "div"
  return (
    <Tag
      {...root}
      data-slot="card"
      className="flex min-w-0 flex-col gap-3 rounded-xl bg-card p-4 text-sm text-card-foreground ring-1 ring-foreground/10"
    >
      <Child id={def.child} ctx={ctx} depth={depth} />
    </Tag>
  )
}

export function TabsComponent({ def, ctx, depth, root }: NodeProps) {
  const tabs = Array.isArray(def.tabs) ? def.tabs.filter(isRecord) : []
  if (!tabs.length) return null
  const { frame, aria } = split(root)
  return (
    <Tabs {...frame} defaultValue="0" className="min-w-0">
      <TabsList
        {...aria}
        className="max-w-full justify-start overflow-x-auto pointer-coarse:h-11!"
      >
        {tabs.map((tab, i) => (
          <TabsTrigger key={i} value={String(i)}>
            {resolveText(tab.title, ctx)}
          </TabsTrigger>
        ))}
      </TabsList>
      {tabs.map((tab, i) => (
        <TabsContent key={i} value={String(i)} className="min-w-0 pt-1">
          <Child id={tab.child} ctx={ctx} depth={depth} />
        </TabsContent>
      ))}
    </Tabs>
  )
}

export function Modal({ def, ctx, depth, root }: NodeProps) {
  const [open, setOpen] = useState(false)
  const { frame, aria } = split(root)
  return (
    <>
      {/* The trigger opens the dialog and does nothing else: its own action,
          if it has one, would otherwise run on the same tap. */}
      <div
        {...frame}
        className="contents"
        onClickCapture={(e) => {
          e.preventDefault()
          e.stopPropagation()
          setOpen(true)
        }}
      >
        <Child id={def.trigger} ctx={ctx} depth={depth} />
      </div>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="max-h-[85dvh] overflow-y-auto sm:max-w-lg">
          <DialogTitle className="sr-only">
            {aria["aria-label"] ?? "Details"}
          </DialogTitle>
          <Child id={def.content} ctx={ctx} depth={depth} />
        </DialogContent>
      </Dialog>
    </>
  )
}

export function Divider({ def, root }: NodeProps) {
  return (
    <Separator
      {...root}
      orientation={def.axis === "vertical" ? "vertical" : "horizontal"}
    />
  )
}

// ── interaction ─────────────────────────────────────────────────────────────

export function ButtonComponent({ def, ctx, depth, root }: NodeProps) {
  const env = useSurface()
  const errors = failingChecks(def.checks, ctx)
  const event =
    isRecord(def.action) && isRecord(def.action.event)
      ? def.action.event
      : undefined
  const sends = event !== undefined
  const danger =
    typeof event?.name === "string" && env.destructive.has(event.name)
  const running =
    sends && env.pending?.id === def.id && env.pending.scope === ctx.scope
  const disabled = errors.length > 0 || (sends && env.busy)
  const onClick = () => env.act(def.action, ctx, def.id)
  const title = errors[0]
  const child = (
    <PhrasingContext value={true}>
      <Child id={def.child} ctx={ctx} depth={depth} />
    </PhrasingContext>
  )
  // The button says it is running; the spinner only shows it. Its flex is
  // its own, past a row's rule that its children share the width.
  const spinner = running && (
    <Spinner
      aria-hidden
      role={undefined}
      aria-label={undefined}
      className="size-3.5"
      style={{ flex: "none" }}
    />
  )
  if (def.variant === "borderless") {
    return (
      <button
        {...root}
        type="button"
        disabled={disabled}
        aria-busy={running || undefined}
        title={title}
        onClick={onClick}
        // A list row's full width, except side by side in a Row, where five
        // of them (a star rating) would otherwise stack.
        className={cn(
          "flex w-full min-w-0 cursor-pointer items-center gap-3 rounded-lg px-3 py-2.5 text-left text-sm transition-colors outline-none focus-visible:ring-2 focus-visible:ring-ring/50 disabled:pointer-events-none disabled:opacity-50 pointer-coarse:min-h-11 [&>*]:min-w-0 [&>*]:flex-1 [.a2ui-row>&]:w-auto",
          danger
            ? "text-destructive hover:bg-destructive/10"
            : "text-foreground hover:bg-hover"
        )}
      >
        {child}
        {spinner}
      </button>
    )
  }
  const primary = def.variant === "primary"
  return (
    <Button
      {...root}
      variant={danger ? "destructive" : primary ? "default" : "outline"}
      disabled={disabled}
      aria-busy={running || undefined}
      data-a2ui-primary={primary || undefined}
      title={title}
      onClick={onClick}
      className="h-auto min-h-9 px-3 py-1.5 whitespace-normal pointer-coarse:min-h-11"
    >
      {spinner}
      {child}
    </Button>
  )
}

/** Enter in a one-line field presses the first primary button after it on
 * the screen, as a form's submit would. That button held back (a check
 * failing, an action running) holds the Enter too, rather than reaching
 * past it to another form's; no such button, and Enter does nothing. */
function submitAfter(e: KeyboardEvent<HTMLInputElement>) {
  if (e.key !== "Enter" || e.nativeEvent.isComposing) return
  const field = e.currentTarget
  const surface = field.closest("[data-a2ui-surface]")
  if (!surface) return
  const next = [
    ...surface.querySelectorAll<HTMLButtonElement>("[data-a2ui-primary]"),
  ].find(
    (button) =>
      field.compareDocumentPosition(button) & Node.DOCUMENT_POSITION_FOLLOWING
  )
  if (!next) return
  e.preventDefault()
  if (next.disabled || next.getAttribute("aria-disabled") === "true") return
  next.click()
}

/** A label, the control, and the messages of the checks that fail once the
 * reader has touched it. */
function Field({
  label,
  htmlFor,
  labelId,
  errors,
  errorId,
  frame,
  children,
  trailing,
}: {
  label: string
  htmlFor?: string
  labelId?: string
  errors: string[]
  errorId: string
  frame: ReturnType<typeof split>["frame"]
  children: ReactNode
  trailing?: ReactNode
}) {
  return (
    <div {...frame} className="flex min-w-0 flex-col gap-1.5">
      {(label || trailing) && (
        <div className="flex items-baseline justify-between gap-3">
          {label && (
            <label
              id={labelId}
              htmlFor={htmlFor}
              className="text-[13px] font-medium text-foreground"
            >
              {label}
            </label>
          )}
          {trailing}
        </div>
      )}
      {children}
      {errors.length > 0 && (
        <div id={errorId} className="flex flex-col gap-0.5">
          {errors.map((message, i) => (
            <p key={i} className="text-xs text-destructive">
              {message}
            </p>
          ))}
        </div>
      )}
    </div>
  )
}

/** A two-way bound value: the data model's at the binding's path, or a
 * value held here when the prop is a literal. */
function useBound(value: unknown, ctx: EvalContext) {
  const env = useSurface()
  const path = bindingPath(value, ctx)
  const bound = resolve(value, ctx)
  const [local, setLocal] = useState<unknown>(bound)
  const [touched, setTouched] = useState(false)
  return {
    value: path ? bound : local,
    touched,
    touch: () => setTouched(true),
    set: (next: unknown) => {
      setTouched(true)
      if (path) env.write(path, next)
      else setLocal(next)
    },
  }
}

export function TextField({ def, ctx, root }: NodeProps) {
  const id = useId()
  const bound = useBound(def.value, ctx)
  const { frame, aria } = split(root)
  const errors = failingChecks(def.checks, ctx)
  const shown = bound.touched ? errors : []
  const variant = typeof def.variant === "string" ? def.variant : "shortText"
  const text = toText(bound.value)
  const control = {
    ...aria,
    id,
    value: text,
    placeholder: resolveText(def.placeholder, ctx) || undefined,
    "aria-invalid": shown.length > 0 || undefined,
    "aria-describedby": shown.length ? `${id}-errors` : undefined,
    onBlur: bound.touch,
  }
  return (
    <Field
      label={resolveText(def.label, ctx)}
      htmlFor={id}
      errors={shown}
      errorId={`${id}-errors`}
      frame={frame}
    >
      {variant === "longText" ? (
        <Textarea
          {...control}
          rows={4}
          onChange={(e) => bound.set(e.target.value)}
        />
      ) : (
        <Input
          {...control}
          type={variant === "obscured" ? "password" : "text"}
          inputMode={variant === "number" ? "decimal" : undefined}
          autoComplete={variant === "obscured" ? "off" : undefined}
          className="pointer-coarse:h-10"
          onKeyDown={variant === "obscured" ? undefined : submitAfter}
          onChange={(e) => {
            const next = e.target.value
            // A number is written as one only when it reads back as typed,
            // so "1." or "-" keep their characters while the reader types.
            const n = variant === "number" ? toNumber(next) : undefined
            bound.set(n !== undefined && String(n) === next.trim() ? n : next)
          }}
        />
      )}
    </Field>
  )
}

export function CheckBox({ def, ctx, root }: NodeProps) {
  const id = useId()
  const bound = useBound(def.value, ctx)
  const { frame, aria } = split(root)
  const errors = failingChecks(def.checks, ctx)
  const shown = bound.touched ? errors : []
  return (
    <div {...frame} className="flex min-w-0 flex-col gap-1">
      <label
        htmlFor={id}
        className="flex cursor-pointer items-center gap-2.5 text-sm text-foreground pointer-coarse:min-h-10"
      >
        <input
          {...aria}
          id={id}
          type="checkbox"
          checked={bound.value === true}
          aria-invalid={shown.length > 0 || undefined}
          onChange={(e) => bound.set(e.target.checked)}
          className="size-4 shrink-0 cursor-pointer accent-primary"
        />
        <span className="min-w-0">{resolveText(def.label, ctx)}</span>
      </label>
      {shown.map((message, i) => (
        <p key={i} className="text-xs text-destructive">
          {message}
        </p>
      ))}
    </div>
  )
}

export function ChoicePicker({ def, ctx, root }: NodeProps) {
  const id = useId()
  const bound = useBound(def.value, ctx)
  const { frame, aria } = split(root)
  const [filter, setFilter] = useState("")
  const errors = failingChecks(def.checks, ctx)
  const shown = bound.touched ? errors : []
  const label = resolveText(def.label, ctx)
  const multiple = def.variant === "multipleSelection"
  const chips = def.displayStyle === "chips"
  const selected = toStringList(bound.value)
  const options = (Array.isArray(def.options) ? def.options : [])
    .filter(isRecord)
    .map((o) => ({
      label: resolveText(o.label, ctx),
      value: toText(o.value),
    }))
  const visible = filter
    ? options.filter((o) =>
        o.label.toLowerCase().includes(filter.trim().toLowerCase())
      )
    : options
  const chosen = (value: string) => selected.includes(value)
  // One chip of a single choice takes the Tab stop: the chosen one, else
  // the first; the arrow keys walk the rest.
  const stop = visible.find((o) => chosen(o.value)) ?? visible[0]
  const walk = (e: KeyboardEvent<HTMLButtonElement>, at: number) => {
    const step =
      e.key === "ArrowRight" || e.key === "ArrowDown"
        ? 1
        : e.key === "ArrowLeft" || e.key === "ArrowUp"
          ? -1
          : 0
    const to =
      e.key === "Home"
        ? 0
        : e.key === "End"
          ? visible.length - 1
          : step
            ? (at + step + visible.length) % visible.length
            : -1
    if (to < 0) return
    e.preventDefault()
    const radios =
      e.currentTarget.parentElement?.querySelectorAll<HTMLElement>(
        "[role=radio]"
      )
    radios?.[to]?.focus()
    bound.set([visible[to].value])
  }
  const choose = (value: string) => {
    if (!multiple) {
      // Chips let the one choice be taken back; radios keep one.
      bound.set(chips && selected.includes(value) ? [] : [value])
      return
    }
    const next = new Set(selected)
    if (next.has(value)) next.delete(value)
    else next.add(value)
    const ordered = options.map((o) => o.value).filter((v) => next.has(v))
    // A chosen value no option offers any more stays chosen.
    const kept = selected.filter(
      (v) => next.has(v) && !options.some((o) => o.value === v)
    )
    bound.set([...ordered, ...kept])
  }
  return (
    <Field
      label={label}
      labelId={`${id}-label`}
      errors={shown}
      errorId={`${id}-errors`}
      frame={frame}
    >
      {def.filterable === true && (
        <Input
          type="search"
          value={filter}
          placeholder="Filter"
          aria-label="Filter the options"
          className="pointer-coarse:h-10"
          onChange={(e) => setFilter(e.target.value)}
        />
      )}
      <div
        {...aria}
        role={multiple ? "group" : "radiogroup"}
        aria-labelledby={
          aria["aria-label"] || !label ? undefined : `${id}-label`
        }
        aria-describedby={shown.length ? `${id}-errors` : undefined}
        className={cn(
          "flex min-w-0",
          chips ? "flex-wrap gap-1.5" : "flex-col gap-1"
        )}
      >
        {visible.map((option, at) =>
          chips ? (
            <button
              key={option.value}
              type="button"
              role={multiple ? undefined : "radio"}
              aria-pressed={multiple ? chosen(option.value) : undefined}
              aria-checked={multiple ? undefined : chosen(option.value)}
              tabIndex={multiple || option === stop ? undefined : -1}
              onKeyDown={multiple ? undefined : (e) => walk(e, at)}
              onClick={() => choose(option.value)}
              className={cn(
                "cursor-pointer rounded-full border px-3 py-1 text-[13px] transition-colors outline-none focus-visible:ring-2 focus-visible:ring-ring/50 pointer-coarse:min-h-10 pointer-coarse:px-4",
                chosen(option.value)
                  ? "border-primary bg-primary text-primary-foreground"
                  : "border-border bg-background text-foreground hover:bg-hover"
              )}
            >
              {option.label}
            </button>
          ) : (
            <label
              key={option.value}
              className="flex cursor-pointer items-center gap-2.5 py-0.5 text-sm text-foreground pointer-coarse:min-h-10"
            >
              <input
                type={multiple ? "checkbox" : "radio"}
                name={multiple ? undefined : id}
                checked={chosen(option.value)}
                onChange={() => choose(option.value)}
                className="size-4 shrink-0 cursor-pointer accent-primary"
              />
              <span className="min-w-0">{option.label}</span>
            </label>
          )
        )}
      </div>
    </Field>
  )
}

export function Slider({ def, ctx, root }: NodeProps) {
  const id = useId()
  const bound = useBound(def.value, ctx)
  const { frame, aria } = split(root)
  const errors = failingChecks(def.checks, ctx)
  const shown = bound.touched ? errors : []
  const min = toNumber(def.min) ?? 0
  const max = toNumber(def.max) ?? 100
  const steps = toNumber(def.steps)
  const step = steps && steps >= 1 && max > min ? (max - min) / steps : "any"
  const value = toNumber(bound.value) ?? min
  return (
    <Field
      label={resolveText(def.label, ctx)}
      htmlFor={id}
      errors={shown}
      errorId={`${id}-errors`}
      frame={frame}
      trailing={
        <output
          htmlFor={id}
          className="text-[13px] text-muted-foreground tabular-nums"
        >
          {value}
        </output>
      }
    >
      <input
        {...aria}
        id={id}
        type="range"
        min={min}
        max={max}
        step={step}
        value={value}
        aria-invalid={shown.length > 0 || undefined}
        onChange={(e) => bound.set(Number(e.target.value))}
        className="w-full cursor-pointer accent-primary pointer-coarse:h-10"
      />
    </Field>
  )
}

type DateMode = "date" | "time" | "datetime-local"

const pad = (n: number) => String(n).padStart(2, "0")

/** An ISO value as the native input reads it. A value with a zone is shown
 * in the reader's own time. */
function inputValue(value: string, mode: DateMode): string {
  if (!value) return ""
  const zoned = /T\d{2}:\d{2}(?::\d{2}(?:\.\d+)?)?(?:Z|[+-]\d{2}:?\d{2})$/i
  if (zoned.test(value)) {
    const d = new Date(value)
    if (!Number.isNaN(d.getTime())) {
      const day = `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
      const time = `${pad(d.getHours())}:${pad(d.getMinutes())}`
      return mode === "date" ? day : mode === "time" ? time : `${day}T${time}`
    }
  }
  const day = /^\d{4}-\d{2}-\d{2}/.exec(value)?.[0]
  const time = /(?:^|T)(\d{2}:\d{2})/.exec(value)?.[1]
  if (mode === "date") return day ?? ""
  if (mode === "time") return time ?? ""
  return day ? `${day}T${time ?? "00:00"}` : ""
}

export function DateTimeInput({ def, ctx, root }: NodeProps) {
  const id = useId()
  const bound = useBound(def.value, ctx)
  const { frame, aria } = split(root)
  const errors = failingChecks(def.checks, ctx)
  const shown = bound.touched ? errors : []
  const date = def.enableDate === true
  const time = def.enableTime === true
  const mode: DateMode =
    date && !time ? "date" : time && !date ? "time" : "datetime-local"
  const bound_ = (v: unknown) => {
    const text = toText(resolve(v, ctx))
    return text ? inputValue(text, mode) || undefined : undefined
  }
  return (
    <Field
      label={resolveText(def.label, ctx)}
      htmlFor={id}
      errors={shown}
      errorId={`${id}-errors`}
      frame={frame}
    >
      <Input
        {...aria}
        id={id}
        type={mode}
        value={inputValue(toText(bound.value), mode)}
        min={bound_(def.min)}
        max={bound_(def.max)}
        aria-invalid={shown.length > 0 || undefined}
        className="pointer-coarse:h-10"
        onBlur={bound.touch}
        onChange={(e) => bound.set(instantValue(e.target.value, mode))}
      />
    </Field>
  )
}

/** What a date-time input writes to the model: the browser's local
 * `YYYY-MM-DDTHH:MM` as the instant it names, in UTC, since a record's
 * datetime takes a zoned instant and inputValue reads one back. A date or
 * a time alone is written as typed. */
function instantValue(value: string, mode: DateMode): string {
  if (mode !== "datetime-local" || !value) return value
  const d = new Date(value)
  return Number.isNaN(d.getTime()) ? value : d.toISOString()
}
