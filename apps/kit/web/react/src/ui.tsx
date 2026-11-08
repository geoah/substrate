import { ChevronLeft, ChevronRight, ChevronsUpDown, X } from "lucide-react"
import { useEffect, type ButtonHTMLAttributes, type ReactNode } from "react"

/** The kit's screen pieces in the Apple apps' grouped style: inset cards of
 * rows, labels left and values right, the same labels in read and edit. */

export function cx(...parts: (string | false | null | undefined)[]): string {
  return parts.filter(Boolean).join(" ")
}

export function Section(props: {
  header?: ReactNode
  footer?: ReactNode
  children: ReactNode
  className?: string
}) {
  return (
    <section className={cx("mb-7", props.className)}>
      {props.header && (
        <h3 className="mb-1.5 px-4 text-[13px] font-normal tracking-wide text-secondary uppercase">
          {props.header}
        </h3>
      )}
      <div className="overflow-hidden rounded-[10px] bg-card [&>*+*]:relative [&>*+*]:before:absolute [&>*+*]:before:inset-x-0 [&>*+*]:before:top-0 [&>*+*]:before:ml-4 [&>*+*]:before:h-px [&>*+*]:before:scale-y-50 [&>*+*]:before:bg-separator">
        {props.children}
      </div>
      {props.footer && (
        <p className="mt-1.5 px-4 text-[13px] text-secondary">{props.footer}</p>
      )}
    </section>
  )
}

/** A label and its value on one row. The value wraps beneath when it is
 * long rather than squeezing the label. */
export function Row(props: {
  label: ReactNode
  value?: ReactNode
  onClick?: () => void
  chevron?: boolean
  valueClassName?: string
}) {
  const inner = (
    <>
      <span className="shrink-0 text-[17px]">{props.label}</span>
      <span
        className={cx(
          "ml-auto min-w-0 text-right text-[17px] break-words text-secondary",
          props.valueClassName
        )}
      >
        {props.value}
      </span>
      {props.chevron && (
        <ChevronRight className="size-4 shrink-0 text-tertiary" />
      )}
    </>
  )
  const cls =
    "flex min-h-11 w-full flex-wrap items-center gap-x-3 gap-y-0.5 px-4 py-2.5 text-left"
  return props.onClick ? (
    <button
      type="button"
      onClick={props.onClick}
      className={cx(cls, "active:bg-selected")}
    >
      {inner}
    </button>
  ) : (
    <div className={cls}>{inner}</div>
  )
}

/** A row whose value is a choice: the platform's own picker over the value. */
export function SelectRow<T extends string>(props: {
  label: ReactNode
  value: T
  options: { value: T; label: string }[]
  onChange: (value: T) => void
  disabled?: boolean
}) {
  const current =
    props.options.find((o) => o.value === props.value)?.label ?? props.value
  return (
    <label className="relative flex min-h-11 w-full items-center gap-3 px-4 py-2.5 active:bg-selected">
      <span className="shrink-0 text-[17px]">{props.label}</span>
      <span className="ml-auto flex min-w-0 items-center gap-1 text-[17px] text-secondary">
        <span className="truncate">{current}</span>
        <ChevronsUpDown className="size-4 shrink-0" />
      </span>
      <select
        className="absolute inset-0 cursor-pointer opacity-0"
        value={props.value}
        disabled={props.disabled}
        onChange={(e) => props.onChange(e.target.value as T)}
        aria-label={typeof props.label === "string" ? props.label : undefined}
      >
        {props.options.map((o) => (
          <option key={o.value} value={o.value}>
            {o.label}
          </option>
        ))}
      </select>
    </label>
  )
}

export function ActionRow(props: {
  children: ReactNode
  onClick: () => void
  destructive?: boolean
  disabled?: boolean
}) {
  return (
    <button
      type="button"
      disabled={props.disabled}
      onClick={props.onClick}
      className={cx(
        "flex min-h-11 w-full items-center px-4 py-2.5 text-left text-[17px] active:bg-selected disabled:opacity-40",
        props.destructive ? "text-danger" : "text-accent-text"
      )}
    >
      {props.children}
    </button>
  )
}

export function Toggle(props: {
  checked: boolean
  onChange: (v: boolean) => void
  label: string
}) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={props.checked}
      aria-label={props.label}
      onClick={() => props.onChange(!props.checked)}
      className={cx(
        "relative h-[31px] w-[51px] shrink-0 rounded-full transition-colors",
        props.checked ? "bg-ok" : "bg-fill"
      )}
    >
      <span
        className={cx(
          "absolute top-[2px] left-[2px] size-[27px] rounded-full bg-white shadow transition-transform",
          props.checked && "translate-x-5"
        )}
      />
    </button>
  )
}

export function ToggleRow(props: {
  label: string
  checked: boolean
  onChange: (v: boolean) => void
}) {
  return (
    <div className="flex min-h-11 items-center gap-3 px-4 py-1.5">
      <span className="text-[17px]">{props.label}</span>
      <span className="ml-auto">
        <Toggle
          label={props.label}
          checked={props.checked}
          onChange={props.onChange}
        />
      </span>
    </div>
  )
}

/** A round glass button for a bar: an icon, or a word. A prominent one is
 * the screen's one confirming action, in the app's accent. */
export function BarButton({
  className,
  prominent,
  ...rest
}: ButtonHTMLAttributes<HTMLButtonElement> & { prominent?: boolean }) {
  return (
    <button
      type="button"
      {...rest}
      className={cx(
        "inline-flex h-11 min-w-11 items-center justify-center rounded-full px-3 text-[17px] disabled:opacity-40",
        prominent
          ? "bg-accent font-semibold text-white active:opacity-80"
          : "bg-card/80 shadow-[0_1px_6px_rgb(0_0_0/0.08)] ring-1 ring-separator/40 backdrop-blur-xl active:bg-selected",
        className
      )}
    />
  )
}

/** The bar over a screen: back, a centred title, actions. */
export function NavBar(props: {
  title?: ReactNode
  onBack?: () => void
  backLabel?: string
  actions?: ReactNode
}) {
  return (
    <div className="sticky top-0 z-20 flex h-[60px] items-center gap-2 bg-gradient-to-b from-grouped via-grouped/90 to-transparent px-4 pt-2">
      <div className="flex min-w-11 flex-1 justify-start">
        {props.onBack && (
          <BarButton
            onClick={props.onBack}
            aria-label={props.backLabel ?? "Back"}
          >
            <ChevronLeft className="size-6" />
          </BarButton>
        )}
      </div>
      {props.title && (
        <div className="min-w-0 truncate text-center text-[17px] font-semibold">
          {props.title}
        </div>
      )}
      <div className="flex min-w-11 flex-1 justify-end gap-2">
        {props.actions}
      </div>
    </div>
  )
}

export function LargeTitle({
  children,
  className,
}: {
  children: ReactNode
  className?: string
}) {
  return (
    <h1
      className={cx(
        "px-4 pt-1 pb-3 text-[34px] leading-tight font-bold tracking-tight",
        className
      )}
    >
      {children}
    </h1>
  )
}

/** A modal: a sheet from the bottom on a phone, a centred card wider. */
export function Sheet(props: {
  open: boolean
  onClose: () => void
  title: ReactNode
  children: ReactNode
  actions?: ReactNode
}) {
  useEffect(() => {
    if (!props.open) return
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && props.onClose()
    window.addEventListener("keydown", onKey)
    return () => window.removeEventListener("keydown", onKey)
  }, [props.open, props.onClose])
  if (!props.open) return null
  return (
    <div
      className="fixed inset-0 z-40 flex items-end justify-center sm:items-center"
      role="dialog"
      aria-modal
    >
      <button
        type="button"
        aria-label="Close"
        className="absolute inset-0 cursor-default bg-black/35"
        onClick={props.onClose}
      />
      <div className="relative flex max-h-[92dvh] w-full flex-col overflow-hidden rounded-t-[14px] bg-grouped shadow-2xl sm:max-w-lg sm:rounded-[14px]">
        <div className="flex h-14 shrink-0 items-center gap-2 px-4">
          <BarButton onClick={props.onClose} aria-label="Cancel">
            <X className="size-5" />
          </BarButton>
          <div className="min-w-0 flex-1 truncate text-center text-[17px] font-semibold">
            {props.title}
          </div>
          <div className="flex min-w-11 justify-end">{props.actions}</div>
        </div>
        <div className="overflow-y-auto px-4 pt-2 pb-[max(1rem,env(safe-area-inset-bottom))]">
          {props.children}
        </div>
      </div>
    </div>
  )
}

/** A question before something that cannot be undone. */
export function Confirm(props: {
  open: boolean
  title: string
  message: ReactNode
  confirm: string
  onConfirm: () => void
  onCancel: () => void
}) {
  if (!props.open) return null
  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center p-6"
      role="alertdialog"
      aria-modal
    >
      <div className="absolute inset-0 bg-black/35" onClick={props.onCancel} />
      <div className="relative w-full max-w-[300px] overflow-hidden rounded-[14px] bg-card/95 text-center shadow-2xl backdrop-blur-xl">
        <div className="px-4 pt-5 pb-4">
          <h2 className="text-[17px] font-semibold">{props.title}</h2>
          <p className="mt-1 text-[13px]">{props.message}</p>
        </div>
        <div className="grid grid-cols-2 border-t border-separator">
          <button
            type="button"
            className="h-11 border-r border-separator text-[17px] text-accent-text"
            onClick={props.onCancel}
          >
            Cancel
          </button>
          <button
            type="button"
            className="h-11 text-[17px] font-semibold text-danger"
            onClick={props.onConfirm}
          >
            {props.confirm}
          </button>
        </div>
      </div>
    </div>
  )
}

/** A text field on a card row, its label above it. */
export function Field(props: {
  label: string
  value: string
  onChange: (v: string) => void
  placeholder?: string
  multiline?: boolean
  type?: string
  autoFocus?: boolean
}) {
  const cls =
    "w-full bg-transparent text-[17px] outline-none placeholder:text-tertiary"
  return (
    <label className="block px-4 py-2.5">
      <span className="block text-[13px] text-secondary">{props.label}</span>
      {props.multiline ? (
        <textarea
          className={cx(cls, "min-h-20 resize-y")}
          value={props.value}
          placeholder={props.placeholder}
          onChange={(e) => props.onChange(e.target.value)}
        />
      ) : (
        <input
          className={cls}
          type={props.type ?? "text"}
          value={props.value}
          placeholder={props.placeholder}
          autoFocus={props.autoFocus}
          onChange={(e) => props.onChange(e.target.value)}
        />
      )}
    </label>
  )
}

/** The bottom bar of a phone: a few top-level sections. */
export function TabBar({ children }: { children: ReactNode }) {
  return (
    <nav className="fixed inset-x-0 bottom-0 z-30 flex justify-center px-6 pb-[max(0.75rem,env(safe-area-inset-bottom))]">
      <div className="flex w-full max-w-sm items-stretch gap-1 rounded-full bg-card/80 p-1.5 shadow-[0_2px_16px_rgb(0_0_0/0.12)] ring-1 ring-separator/40 backdrop-blur-xl">
        {children}
      </div>
    </nav>
  )
}

export function TabItem(props: {
  icon: ReactNode
  label: string
  active: boolean
  badge?: number
  onClick: () => void
}) {
  return (
    <button
      type="button"
      onClick={props.onClick}
      aria-current={props.active ? "page" : undefined}
      className={cx(
        "relative flex flex-1 flex-col items-center justify-center gap-0.5 rounded-full py-1.5 text-[11px] font-medium",
        props.active ? "bg-selected text-accent-text" : "text-label"
      )}
    >
      <span className="relative">
        {props.icon}
        {!!props.badge && (
          <span className="absolute -top-1.5 -right-3 min-w-[18px] rounded-full bg-danger px-1 text-[11px] leading-[18px] text-white">
            {props.badge}
          </span>
        )}
      </span>
      {props.label}
    </button>
  )
}

/** One row of a sidebar: an icon, a label and a count. */
export function SidebarItem(props: {
  icon: ReactNode
  label: string
  count?: number
  active?: boolean
  onClick: () => void
}) {
  return (
    <button
      type="button"
      onClick={props.onClick}
      aria-current={props.active ? "page" : undefined}
      className={cx(
        "flex h-11 w-full items-center gap-3 rounded-[10px] px-3 text-left text-[17px]",
        props.active ? "bg-selected" : "active:bg-selected"
      )}
    >
      <span className="text-accent-text">{props.icon}</span>
      <span className="min-w-0 flex-1 truncate">{props.label}</span>
      {props.count !== undefined && props.count > 0 && (
        <span className="text-secondary">{props.count}</span>
      )}
    </button>
  )
}

/** A line under the bar while the link is not live. */
export function LinkStatus(props: { status: string; words?: string }) {
  if (props.status === "live") return null
  const text =
    props.status === "loading"
      ? "Loading…"
      : (props.words ??
        (props.status === "reconnecting"
          ? "Reconnecting…"
          : "Can't reach your server."))
  return (
    <div
      className="mx-4 mb-2 rounded-[10px] bg-fill px-3 py-2 text-[13px] text-secondary"
      role="status"
    >
      {text}
    </div>
  )
}
