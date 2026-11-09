/** A record view's conversation block: the records of a kind that point at
 * this one, read as a conversation in time order (a chat's messages), with
 * each message's sender, time, text, attachments, reactions and its edited,
 * deleted and send marks, and, where the view names a composer, a box to
 * reply, react and mark read. A withdrawn message keeps what it said, under
 * a mark that says it was withdrawn, and takes no reply or reaction. Each
 * of those writes a request record of the composer's kind as the owner; the
 * requests are listed with who asked and their state, a queued one can be
 * cancelled and a failed one sent again as a new request, and the ones a
 * rule holds for the owner (an agent's, whichever the rule holds) can be
 * applied or dismissed here. Every kind and property is the view's
 * (lib/conversation.ts); the console knows none of them. */

import { Fragment, useMemo, useState, type ReactNode } from "react"
import {
  useInfiniteQuery,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import {
  CornerUpLeftIcon,
  EllipsisIcon,
  SmilePlusIcon,
  Trash2Icon,
  XIcon,
} from "lucide-react"

import {
  Failed,
  Loading,
  Notice,
  Part,
  Quiet,
  type BlockContext,
} from "./parts"
import { AgentRef } from "@/components/agent/agent-ref"
import { DecisionButtons } from "@/components/change-request"
import { ActorRef } from "@/components/identity/actor-ref"
import { IdText } from "@/components/identity/id-text"
import { RecordRef } from "@/components/identity/record-ref"
import { StateBadge } from "@/components/identity/state-badge"
import { ago, friendlyDay } from "@/components/property-sheet/dates"
import { DeclaredValue } from "@/components/property-sheet/property-value"
import { writeError } from "@/components/property-sheet/use-record-patch"
import { Button } from "@/components/ui/button"
import { ConfirmDialog } from "@/components/ui/confirm-dialog"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Spinner } from "@/components/ui/spinner"
import { Textarea } from "@/components/ui/textarea"
import { toast } from "@/components/ui/toast"
import { useTechnicalDetails } from "@/hooks/use-console-preferences"
import { useLiveRecords } from "@/hooks/use-live-records"
import { useReferenceTitles } from "@/hooks/use-reference-titles"
import { RECORD_PATCH_REQUEST_KIND } from "@/lib/agent-grants"
import { agentActor, requestThreadId, threadAgentId } from "@/lib/agent-chat"
import { changeRequestQueryOptions } from "@/lib/api/changerequests"
import {
  CORE_AUTHORITY,
  LLM_PACKAGE_NAME,
  serverNow,
  splitKind,
} from "@/lib/api/http"
import {
  createRecord,
  fetchRecordsPage,
  patchRecord,
  recordQueryOptions,
  recordsQueryOptions,
  recordWriteReaches,
} from "@/lib/api/records"
import type { KindInfo, Page, SubstrateRecord } from "@/lib/api/types"
import { proposerOf, rationaleOf } from "@/lib/changerequests"
import {
  askAgain,
  attachmentsParams,
  cancelAsked,
  canCancel,
  canRetry,
  conversationOrder,
  deletedMark,
  heldFor,
  heldParams,
  messagesParams,
  messageViewOf,
  newestMessage,
  offerOf,
  pathOf,
  referencePath,
  requestProperties,
  requestState,
  requesterOf,
  requestsParams,
  sendNeedsConfirm,
  type Ask,
  type MessageView,
} from "@/lib/conversation"
import { kindByIdentity } from "@/lib/definition"
import { outcomeUnknown } from "@/lib/state-words"
import { recordTitle } from "@/lib/format"
import { displayPlural, lowerFirst } from "@/lib/kind-names"
import { clockWords } from "@/lib/observations-time"
import { splitRecordPath } from "@/lib/record-path"
import { propSpecs, type PropSpec } from "@/lib/record-schema"
import type { ComposeSpec, ConversationBlock } from "@/lib/record-view"
import { cn } from "@/lib/utils"

/** The reactions the composer offers on any message. A key a message
 * already holds is offered too, from its chip. */
const REACTIONS = ["👍", "❤️", "😂", "😮", "😢", "🙏"]

/** How much of a message a quote or a request line shows. */
const EXCERPT = 90

export function ConversationBlockView({
  block,
  ctx,
}: {
  block: ConversationBlock
  ctx: BlockContext
}) {
  const kinds = [...ctx.kinds]
  const messageKind = kindByIdentity(kinds, block.kind)
  const requestKind = block.compose
    ? kindByIdentity(kinds, block.compose.kind)
    : undefined
  const unknown = [
    !messageKind && block.kind,
    block.attachments &&
      !kindByIdentity(kinds, block.attachments) &&
      block.attachments,
    block.compose && !requestKind && block.compose.kind,
  ].filter((k): k is string => Boolean(k))
  if (!messageKind || unknown.length) {
    return (
      <Notice>
        This conversation names {unknown.length === 1 ? "a kind" : "kinds"} this
        repository does not declare: {unknown.join(", ")}.
      </Notice>
    )
  }
  return (
    <Conversation
      block={block}
      ctx={ctx}
      messageKind={messageKind}
      requestKind={requestKind}
    />
  )
}

function specOf(
  kind: KindInfo | undefined,
  name: string | undefined
): PropSpec | undefined {
  if (!kind || !name) return undefined
  return propSpecs(kind).find((s) => s.name === name)
}

/** A declared value's label, else the value as stored. */
function valueLabel(spec: PropSpec | undefined, value: string): string {
  return spec?.values?.find((v) => v.value === value)?.label ?? value
}

function excerpt(text: string | undefined): string | undefined {
  if (!text) return undefined
  const one = text.replace(/\s+/g, " ").trim()
  return one.length > EXCERPT ? `${one.slice(0, EXCERPT - 1)}…` : one
}

function instant(iso: string | undefined): number | undefined {
  if (!iso) return undefined
  const t = Date.parse(iso)
  return Number.isNaN(t) ? undefined : t
}

function dayKey(iso: string | undefined): string {
  const t = instant(iso)
  if (t === undefined) return ""
  const d = new Date(t)
  return `${d.getFullYear()}-${d.getMonth()}-${d.getDate()}`
}

/** Every read the block makes is keyed under `records` and its kind, so a
 * write elsewhere that reaches the kind (recordWriteReaches) and the live
 * feed both re-read it. */
function messagesKey(block: ConversationBlock, record: SubstrateRecord) {
  return [
    "records",
    [block.kind],
    {
      conversation: pathOf(record),
      via: block.via,
      time: block.time,
      first: block.limit,
    },
  ] as const
}

function Conversation({
  block,
  ctx,
  messageKind,
  requestKind,
}: {
  block: ConversationBlock
  ctx: BlockContext
  messageKind: KindInfo
  requestKind?: KindInfo
}) {
  const record = ctx.record
  const compose = block.compose
  const kinds = useMemo(() => [...ctx.kinds], [ctx.kinds])
  const live = useMemo(
    () =>
      [
        block.kind,
        block.attachments,
        compose?.kind,
        compose ? RECORD_PATCH_REQUEST_KIND : undefined,
      ].filter((k): k is string => Boolean(k)),
    [block.kind, block.attachments, compose]
  )
  useLiveRecords(
    live,
    live.map((k) => ["records", [k]])
  )

  const pages = useInfiniteQuery({
    queryKey: messagesKey(block, record),
    queryFn: ({ pageParam, signal }) =>
      fetchRecordsPage(messagesParams(block, record, pageParam), signal),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last: Page) => last.cursor || undefined,
  })
  const messages = useMemo(
    () =>
      conversationOrder(
        block,
        (pages.data?.pages ?? []).map((p) => p.records)
      ),
    [block, pages.data]
  )
  const included = useMemo(() => {
    const out = new Map<string, SubstrateRecord>()
    for (const page of pages.data?.pages ?? []) {
      for (const [path, r] of Object.entries(page.included ?? {})) {
        out.set(path, r)
      }
    }
    for (const m of messages) out.set(m.path, m.record)
    return out
  }, [pages.data, messages])

  const attachmentKind = block.attachments
    ? kindByIdentity(kinds, block.attachments)
    : undefined
  const attachmentRead = attachmentsParams(
    block,
    messages.map((m) => m.path)
  )
  const attachments = useQuery({
    ...recordsQueryOptions(attachmentRead ?? { kinds: [] }),
    enabled: Boolean(attachmentRead && attachmentKind),
  })
  const attachmentsOf = useMemo(() => {
    const out = new Map<string, SubstrateRecord[]>()
    for (const a of attachments.data?.records ?? []) {
      const owner = referencePath(a.properties, block.attachmentVia)
      if (!owner) continue
      out.set(owner, [...(out.get(owner) ?? []), a])
    }
    return out
  }, [attachments.data, block.attachmentVia])

  // Who reacted, by name, in one batched read for the whole page.
  const reactors = useMemo(
    () => [
      ...new Set(messages.flatMap((m) => m.reactions.flatMap((r) => r.users))),
    ],
    [messages]
  )
  const titles = useReferenceTitles(reactors, kinds)

  const [replying, setReplying] = useState<MessageView>()
  const asker = useAsker(compose, record, requestKind)

  const readUpTo = referencePath(record.properties, block.readUpTo)
  const specs = useMemo(
    () => ({
      status: specOf(messageKind, block.status),
      deleted: specOf(messageKind, block.deleted),
      file: specOf(attachmentKind, block.attachmentFile),
    }),
    [
      messageKind,
      attachmentKind,
      block.status,
      block.deleted,
      block.attachmentFile,
    ]
  )
  const plural = displayPlural(messageKind)

  return (
    <Part title={block.label}>
      {pages.isPending ? (
        <Loading />
      ) : pages.error ? (
        <Failed error={pages.error} onRetry={() => void pages.refetch()} />
      ) : (
        <div className="flex max-w-[720px] flex-col gap-2">
          {pages.hasNextPage && (
            <Button
              variant="ghost"
              size="sm"
              className="self-center"
              disabled={pages.isFetchingNextPage}
              onClick={() => void pages.fetchNextPage()}
            >
              {pages.isFetchingNextPage && <Spinner className="size-3" />}
              Show earlier {lowerFirst(plural)}
            </Button>
          )}
          {!messages.length ? (
            <Quiet>No {lowerFirst(plural)} here yet.</Quiet>
          ) : (
            <ol aria-label={plural} className="flex flex-col gap-1.5">
              {messages.map((m, i) => {
                const prev = messages[i - 1]
                const newDay = !prev || dayKey(prev.at) !== dayKey(m.at)
                const named =
                  !m.mine &&
                  Boolean(m.sender) &&
                  (newDay || prev?.mine || prev?.sender !== m.sender)
                return (
                  <Fragment key={m.path}>
                    {newDay && m.at && (
                      <li
                        data-slot="conversation-day"
                        className="mt-2 self-center text-[11.5px] font-medium text-faint"
                      >
                        {friendlyDay(m.at)}
                      </li>
                    )}
                    <MessageRow
                      message={m}
                      named={named}
                      block={block}
                      compose={compose}
                      included={included}
                      attachments={attachmentsOf.get(m.path) ?? []}
                      specs={specs}
                      titles={titles}
                      onReply={
                        compose?.send && compose.replyTo
                          ? () => setReplying(m)
                          : undefined
                      }
                      asker={asker}
                    />
                    {readUpTo === m.path && i < messages.length - 1 && (
                      <li
                        data-slot="conversation-read-mark"
                        className="flex items-center gap-2 text-[11.5px] text-faint"
                      >
                        <span className="h-px flex-1 bg-border" />
                        You read up to here
                        <span className="h-px flex-1 bg-border" />
                      </li>
                    )}
                  </Fragment>
                )
              })}
            </ol>
          )}
          {compose && requestKind && (
            <Composer
              compose={compose}
              noun={ctx.noun}
              newest={newestMessage(messages)}
              replying={replying}
              onStopReplying={() => setReplying(undefined)}
              asker={asker}
            />
          )}
        </div>
      )}
      {compose && requestKind && (
        <>
          <Held
            compose={compose}
            record={record}
            requestKind={requestKind}
            messages={included}
            block={block}
          />
          <Requests
            compose={compose}
            record={record}
            requestKind={requestKind}
            messages={included}
            block={block}
            asker={asker}
          />
        </>
      )}
      {asker.dialog}
    </Part>
  )
}

// ── asking ──────────────────────────────────────────────────────────────────

interface Asker {
  /** Writes one request, asking first where a send reaches someone outside
   * the owner's contacts or the request may already have been sent. */
  ask: (
    ask: Ask,
    opts?: { again?: SubstrateRecord; onDone?: () => void }
  ) => void
  pending: boolean
  /** The confirmation open now, if any. */
  dialog: ReactNode
}

function useAsker(
  compose: ComposeSpec | undefined,
  record: SubstrateRecord,
  requestKind: KindInfo | undefined
): Asker {
  const client = useQueryClient()
  const peerPath = compose
    ? referencePath(record.properties, compose.peer)
    : undefined
  const peerParts = peerPath ? splitRecordPath(peerPath) : undefined
  const peerKind = peerParts ? splitKind(peerParts.kind) : undefined
  const peer = useQuery({
    ...recordQueryOptions(
      peerKind?.authority ?? "",
      peerKind?.pkg ?? "",
      peerKind?.name ?? "",
      peerParts?.id ?? ""
    ),
    enabled: Boolean(peerKind && peerParts && compose?.peerKnown),
  })
  const [pendingAsk, setPendingAsk] = useState<{
    ask: Ask
    again?: SubstrateRecord
    stranger: boolean
    onDone?: () => void
  }>()
  const write = useMutation({
    mutationFn: async (properties: Record<string, unknown>) => {
      const { authority, pkg, name } = splitKind(requestKind?.identity ?? "")
      return createRecord(authority, pkg, name, { properties })
    },
    onSuccess: (saved) =>
      client.invalidateQueries({
        predicate: (q) => recordWriteReaches(q.queryKey, saved.kind, saved.id),
      }),
  })

  function send(ask: Ask, onDone?: () => void) {
    if (!compose) return
    const properties = requestProperties(compose, record, ask)
    if (typeof properties === "string") {
      toast.add({ type: "error", title: properties })
      return
    }
    write.mutate(properties, {
      onSuccess: () => {
        setPendingAsk(undefined)
        onDone?.()
      },
      onError: (error) => {
        if (!pendingAsk)
          toast.add({
            type: "error",
            title: "That request wasn’t written",
            description: writeError(error),
          })
      },
    })
  }

  function ask(
    next: Ask,
    opts?: { again?: SubstrateRecord; onDone?: () => void }
  ) {
    if (!compose) return
    const stranger =
      next.offer === "send" && sendNeedsConfirm(compose, record, peer.data)
    if (stranger || opts?.again) {
      write.reset()
      setPendingAsk({
        ask: next,
        again: opts?.again,
        stranger,
        onDone: opts?.onDone,
      })
      return
    }
    send(next, opts?.onDone)
  }

  const unknown =
    pendingAsk?.again && compose
      ? outcomeUnknown(requestState(compose, pendingAsk.again))
      : false
  const peerName = peer.data ? recordTitle(peer.data.properties) : undefined
  const dialog = pendingAsk && compose && (
    <ConfirmDialog
      title={
        pendingAsk.again
          ? pendingAsk.ask.offer === "send"
            ? "Send this again?"
            : "Ask for this again?"
          : "Send to someone outside your contacts?"
      }
      consequence={
        <>
          {unknown &&
            "Whether the first one went through isn’t known, so it may already have been sent. "}
          {pendingAsk.again &&
            "This writes a new request with the same values. "}
          {pendingAsk.stranger &&
            `${peerName || "This person"} isn’t among your contacts there, so sending starts a conversation as you.`}
        </>
      }
      confirm={pendingAsk.ask.offer === "send" ? "Send" : "Ask again"}
      pending={write.isPending}
      error={write.error ? writeError(write.error) : undefined}
      onConfirm={() =>
        send(
          { ...pendingAsk.ask, confirmed: pendingAsk.stranger || undefined },
          pendingAsk.onDone
        )
      }
      onClose={() => setPendingAsk(undefined)}
    />
  )
  return { ask, pending: write.isPending, dialog }
}

// ── the messages ────────────────────────────────────────────────────────────

function MessageRow({
  message: m,
  named,
  block,
  compose,
  included,
  attachments,
  specs,
  titles,
  onReply,
  asker,
}: {
  message: MessageView
  named: boolean
  block: ConversationBlock
  compose?: ComposeSpec
  included: ReadonlyMap<string, SubstrateRecord>
  attachments: readonly SubstrateRecord[]
  specs: { status?: PropSpec; deleted?: PropSpec; file?: PropSpec }
  titles: ReadonlyMap<string, string>
  onReply?: () => void
  asker: Asker
}) {
  const t = instant(m.at)
  const sender = m.sender ? splitRecordPath(m.sender) : undefined
  const senderRecord = m.sender ? included.get(m.sender) : undefined
  const react = (key: string) =>
    asker.ask({ offer: "react", target: m.path, reaction: key })
  return (
    <li
      id={`message-${m.record.id}`}
      data-slot="conversation-message"
      data-mine={m.mine || undefined}
      className={cn(
        "group flex flex-col gap-0.5",
        m.mine ? "items-end" : "items-start"
      )}
    >
      {named && sender && (
        <span className="text-[12px] text-muted-foreground">
          <RecordRef
            kind={sender.kind}
            id={sender.id}
            title={
              senderRecord
                ? recordTitle(senderRecord.properties) || undefined
                : undefined
            }
          />
        </span>
      )}
      <div
        className={cn(
          "flex max-w-[85%] items-start gap-1",
          m.mine && "flex-row-reverse"
        )}
      >
        <div
          data-deleted={m.deleted ? "true" : undefined}
          className={cn(
            "min-w-0 rounded-lg px-3 py-2 text-[13.5px]",
            m.deleted
              ? "border border-dashed bg-transparent text-muted-foreground"
              : m.mine
                ? "bg-primary-soft"
                : "border bg-panel"
          )}
        >
          {m.deleted && (
            <p
              data-slot="conversation-deleted"
              className="mb-1 flex items-center gap-1 text-[12px] font-medium"
            >
              <Trash2Icon aria-hidden className="size-3.5 shrink-0" />
              {deletedMark(specs.deleted, m.deleted)}
            </p>
          )}
          {m.replyTo && (
            <ReplyQuote
              path={m.replyTo}
              block={block}
              included={included}
              deletedSpec={specs.deleted}
            />
          )}
          {m.text && (
            <p className="break-words whitespace-pre-wrap">{m.text}</p>
          )}
          {attachments.length > 0 && (
            <ul aria-label="Attachments" className="mt-1.5 flex flex-col gap-1">
              {attachments.map((a) => (
                <li key={a.id}>
                  <Attachment record={a} block={block} spec={specs.file} />
                </li>
              ))}
            </ul>
          )}
        </div>
        {compose && (
          <MessageMenu
            message={m}
            compose={compose}
            onReply={m.deleted ? undefined : onReply}
            onReact={react}
            onMarkRead={() => asker.ask({ offer: "markRead", target: m.path })}
          />
        )}
      </div>
      <div className="flex flex-wrap items-center gap-x-2 text-[11.5px] text-faint">
        {t !== undefined && (
          <time dateTime={m.at} title={m.at}>
            {clockWords(t)}
          </time>
        )}
        {m.edited && (
          <span
            data-slot="conversation-edited"
            title={typeof m.edited === "string" ? m.edited : undefined}
          >
            Edited
          </span>
        )}
        {m.mine && m.status && (
          <span data-slot="conversation-status">
            {valueLabel(specs.status, m.status)}
          </span>
        )}
      </div>
      {m.reactions.length > 0 && (
        <ul aria-label="Reactions" className="flex flex-wrap gap-1">
          {m.reactions.map((r) => {
            const who = r.users.map((u) => titles.get(u)).filter(Boolean)
            const label = `${r.key} ${r.count}${who.length ? `: ${who.join(", ")}` : ""}`
            return (
              <li key={r.key}>
                {compose?.react && !m.deleted ? (
                  <button
                    type="button"
                    data-slot="conversation-reaction"
                    title={label}
                    aria-label={`React with ${r.key} too; ${label}`}
                    className="cursor-pointer rounded-full border bg-background px-1.5 text-[12px] tabular-nums hover:bg-hover"
                    onClick={() => react(r.key)}
                  >
                    {r.key} {r.count}
                  </button>
                ) : (
                  <span
                    data-slot="conversation-reaction"
                    title={label}
                    className="rounded-full border bg-background px-1.5 text-[12px] tabular-nums"
                  >
                    {r.key} {r.count}
                  </span>
                )}
              </li>
            )
          })}
        </ul>
      )}
    </li>
  )
}

function ReplyQuote({
  path,
  block,
  included,
  deletedSpec,
}: {
  path: string
  block: ConversationBlock
  included: ReadonlyMap<string, SubstrateRecord>
  deletedSpec?: PropSpec
}) {
  const answered = included.get(path)
  const view = answered ? messageViewOf(block, answered) : undefined
  const said = excerpt(view?.text)
  const parts = splitRecordPath(path)
  return (
    <div
      data-slot="conversation-reply"
      className="mb-1.5 border-l-2 pl-2 text-[12.5px] text-muted-foreground"
    >
      <span className="sr-only">In reply to: </span>
      {view?.deleted && (
        <span className="italic">
          {deletedMark(deletedSpec, view.deleted)}:{" "}
        </span>
      )}
      {said ? (
        <a
          href={`#message-${parts?.id ?? ""}`}
          className="underline-offset-2 hover:underline"
        >
          {said}
        </a>
      ) : parts ? (
        <RecordRef kind={parts.kind} id={parts.id} />
      ) : null}
    </div>
  )
}

function Attachment({
  record,
  block,
  spec,
}: {
  record: SubstrateRecord
  block: ConversationBlock
  spec?: PropSpec
}) {
  const file = block.attachmentFile
    ? record.properties?.[block.attachmentFile]
    : undefined
  if (spec && file !== undefined && file !== null && file !== "") {
    return <DeclaredValue spec={spec} value={file} />
  }
  return (
    <RecordRef
      kind={record.kind}
      id={record.id}
      title={recordTitle(record.properties) || undefined}
      variant="chip"
    />
  )
}

function MessageMenu({
  message,
  compose,
  onReply,
  onReact,
  onMarkRead,
}: {
  message: MessageView
  compose: ComposeSpec
  onReply?: () => void
  onReact: (key: string) => void
  onMarkRead: () => void
}) {
  // Nobody answers or reacts to what its sender withdrew; reading up to it
  // is still the reader's own.
  const react = Boolean(compose.react) && !message.deleted
  const read = Boolean(compose.markRead)
  if (!onReply && !react && !read) return null
  const what = excerpt(message.text) ?? "this message"
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label={`Act on “${what}”`}
            className="opacity-0 group-hover:opacity-100 focus-visible:opacity-100 data-[popup-open]:opacity-100 [@media(hover:none)]:opacity-100"
          />
        }
      >
        <EllipsisIcon className="size-4" />
      </DropdownMenuTrigger>
      <DropdownMenuContent align={message.mine ? "end" : "start"}>
        {onReply && (
          <DropdownMenuItem onClick={onReply}>
            <CornerUpLeftIcon /> Reply
          </DropdownMenuItem>
        )}
        {react && (
          <>
            {onReply && <DropdownMenuSeparator />}
            {REACTIONS.map((key) => (
              <DropdownMenuItem key={key} onClick={() => onReact(key)}>
                <SmilePlusIcon /> React with {key}
              </DropdownMenuItem>
            ))}
          </>
        )}
        {read && (
          <>
            {(onReply || react) && <DropdownMenuSeparator />}
            <DropdownMenuItem onClick={onMarkRead}>
              Mark read up to here
            </DropdownMenuItem>
          </>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

// ── the composer ────────────────────────────────────────────────────────────

function Composer({
  compose,
  noun,
  newest,
  replying,
  onStopReplying,
  asker,
}: {
  compose: ComposeSpec
  noun: string
  newest?: MessageView
  replying?: MessageView
  onStopReplying: () => void
  asker: Asker
}) {
  const [draft, setDraft] = useState("")
  const send = () => {
    if (!draft.trim()) return
    asker.ask(
      { offer: "send", text: draft, replyTo: replying?.path },
      {
        onDone: () => {
          setDraft("")
          onStopReplying()
        },
      }
    )
  }
  return (
    <form
      aria-label={`Write in this ${noun}`}
      className="mt-1 flex flex-col gap-1.5 rounded-lg border p-2"
      onSubmit={(e) => {
        e.preventDefault()
        send()
      }}
    >
      {replying && (
        <div
          data-slot="composer-replying"
          className="flex items-start gap-2 border-l-2 pl-2 text-[12.5px] text-muted-foreground"
        >
          <span className="min-w-0 flex-1 break-words">
            Replying to “{excerpt(replying.text) ?? "a message"}”
          </span>
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            aria-label="Stop replying"
            onClick={onStopReplying}
          >
            <XIcon className="size-3.5" />
          </Button>
        </div>
      )}
      {compose.send && (
        <Textarea
          aria-label="Message"
          placeholder="Write a message"
          value={draft}
          rows={2}
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={(e) => {
            if (
              e.key === "Enter" &&
              !e.shiftKey &&
              !e.nativeEvent.isComposing
            ) {
              e.preventDefault()
              send()
            }
          }}
        />
      )}
      <div className="flex flex-wrap items-center justify-end gap-1.5">
        {compose.send && (
          <span className="mr-auto text-[11.5px] text-faint">
            Sends as you. Enter sends, Shift+Enter adds a line.
          </span>
        )}
        {compose.markRead && newest && (
          <Button
            type="button"
            variant="ghost"
            size="sm"
            disabled={asker.pending}
            onClick={() =>
              asker.ask({ offer: "markRead", target: newest.path })
            }
          >
            Mark as read
          </Button>
        )}
        {compose.send && (
          <Button
            type="submit"
            size="sm"
            disabled={asker.pending || !draft.trim()}
          >
            {asker.pending && <Spinner className="size-3" />}
            Send
          </Button>
        )}
      </div>
    </form>
  )
}

// ── the requests ────────────────────────────────────────────────────────────

/** What a request asks, in a line: the text it sends, the reaction and the
 * message it is on, or the message a read mark reaches. */
function RequestSummary({
  compose,
  properties,
  messages,
  block,
}: {
  compose: ComposeSpec
  properties: Record<string, unknown>
  messages: ReadonlyMap<string, SubstrateRecord>
  block: ConversationBlock
}) {
  const offer = offerOf(compose, properties)
  const target = referencePath(properties, compose.target)
  const targetText = (() => {
    if (!target) return undefined
    const m = messages.get(target)
    return excerpt(
      block.text && m
        ? (m.properties?.[block.text] as string | undefined)
        : undefined
    )
  })()
  const targetRef = (() => {
    if (!target) return null
    if (targetText) return <q>{targetText}</q>
    const parts = splitRecordPath(target)
    return parts ? <RecordRef kind={parts.kind} id={parts.id} /> : null
  })()
  const sent =
    compose.text && typeof properties[compose.text] === "string"
      ? excerpt(properties[compose.text] as string)
      : undefined
  const reaction =
    compose.reaction && typeof properties[compose.reaction] === "string"
      ? (properties[compose.reaction] as string)
      : undefined
  switch (offer) {
    case "send":
      return sent ? <q className="break-words">{sent}</q> : null
    case "react":
      return (
        <span className="inline-flex flex-wrap items-baseline gap-1">
          {reaction} <span className="text-muted-foreground">on</span>{" "}
          {targetRef}
        </span>
      )
    case "markRead":
      return (
        <span className="inline-flex flex-wrap items-baseline gap-1">
          <span className="text-muted-foreground">up to</span> {targetRef}
        </span>
      )
    default:
      return null
  }
}

function verbLabel(
  compose: ComposeSpec,
  requestKind: KindInfo,
  properties: Record<string, unknown>
): string {
  const value = properties[compose.verb]
  return typeof value === "string"
    ? valueLabel(specOf(requestKind, compose.verb), value)
    : "Request"
}

function Requests({
  compose,
  record,
  requestKind,
  messages,
  block,
  asker,
}: {
  compose: ComposeSpec
  record: SubstrateRecord
  requestKind: KindInfo
  messages: ReadonlyMap<string, SubstrateRecord>
  block: ConversationBlock
  asker: Asker
}) {
  const [technical] = useTechnicalDetails()
  const page = useQuery(recordsQueryOptions(requestsParams(compose, record)))
  const stateSpec = specOf(requestKind, compose.state)
  const errorSpec = specOf(requestKind, compose.error)
  const client = useQueryClient()
  const cancel = useMutation({
    mutationFn: (request: SubstrateRecord) => {
      const { authority, pkg, name } = splitKind(request.kind)
      return patchRecord(authority, pkg, name, request.id, {
        properties: { [compose.cancel ?? ""]: serverNow().toISOString() },
        ifVersion: request.version,
      })
    },
    onSuccess: (saved) =>
      client.invalidateQueries({
        predicate: (q) => recordWriteReaches(q.queryKey, saved.kind, saved.id),
      }),
    onError: (error) =>
      toast.add({
        type: "error",
        title: "Cancelling didn’t go through",
        description: writeError(error),
      }),
  })
  const records = page.data?.records ?? []
  if (page.isPending || (!records.length && !page.error)) return null
  const title = displayPlural(requestKind)
  return (
    <div
      role="group"
      aria-label={title}
      data-slot="conversation-requests"
      className="mt-4 flex max-w-[720px] flex-col gap-1.5"
    >
      <h4 className="text-[13px] font-medium">{title}</h4>
      <p className="text-[12.5px] text-muted-foreground">
        What you and your agents asked for here, newest first, with who asked
        and how each went.
      </p>
      {page.error ? (
        <Failed error={page.error} onRetry={() => void page.refetch()} />
      ) : (
        <ul className="flex flex-col divide-y rounded-[10px] border">
          {records.map((r) => {
            const p = r.properties ?? {}
            const state = requestState(compose, r)
            const error =
              compose.error && typeof p[compose.error] === "string"
                ? (p[compose.error] as string)
                : undefined
            const asked = cancelAsked(compose, r)
            const again = askAgain(compose, r)
            return (
              <li
                key={r.id}
                data-slot="conversation-request"
                className="grid grid-cols-[minmax(0,1fr)_auto] items-start gap-x-3 gap-y-1 px-3 py-2 text-[13px]"
              >
                <div className="flex min-w-0 flex-col gap-0.5">
                  <span className="flex flex-wrap items-baseline gap-x-1.5">
                    <span className="font-medium">
                      {verbLabel(compose, requestKind, p)}
                    </span>
                    <RequestSummary
                      compose={compose}
                      properties={p}
                      messages={messages}
                      block={block}
                    />
                  </span>
                  <span className="flex flex-wrap items-center gap-x-2 text-[12px] text-muted-foreground">
                    {state && (
                      <StateBadge value={state} initial={stateSpec?.initial} />
                    )}
                    {asked && state && compose.cancelIn.includes(state) && (
                      <span>Cancelling</span>
                    )}
                    <span title={r.createdAt}>Asked {ago(r.createdAt)}</span>
                    <Requester compose={compose} request={r} />
                  </span>
                  {error && (
                    <span className="text-[12.5px] break-words">
                      <span className="text-faint">
                        {errorSpec?.label ?? "Why"}:
                      </span>{" "}
                      {error}
                    </span>
                  )}
                  {technical && <IdText value={pathOf(r)} copy />}
                </div>
                <div className="flex items-center gap-1">
                  {canCancel(compose, r) && (
                    <Button
                      variant="outline"
                      size="sm"
                      disabled={cancel.isPending}
                      onClick={() => cancel.mutate(r)}
                    >
                      Cancel
                    </Button>
                  )}
                  {canRetry(compose, r) && again && (
                    <Button
                      variant="outline"
                      size="sm"
                      disabled={asker.pending}
                      onClick={() => asker.ask(again, { again: r })}
                    >
                      {again.offer === "send" ? "Send again" : "Ask again"}
                    </Button>
                  )}
                </div>
              </li>
            )
          })}
        </ul>
      )}
    </div>
  )
}

/** Who asked for one request: the owner, or the agent whose request no rule
 * held. The list read carries no provenance, so each row reads its own. */
function Requester({
  compose,
  request,
}: {
  compose: ComposeSpec
  request: SubstrateRecord
}) {
  const { authority, pkg, name } = splitKind(request.kind)
  const single = useQuery(recordQueryOptions(authority, pkg, name, request.id))
  const actor = single.data ? requesterOf(compose, single.data) : undefined
  if (!actor) return null
  return (
    <span
      data-slot="conversation-requester"
      className="inline-flex items-center gap-1"
    >
      by <ActorRef actor={actor} />
    </span>
  )
}

// ── held for the owner ──────────────────────────────────────────────────────

function Held({
  compose,
  record,
  requestKind,
  messages,
  block,
}: {
  compose: ComposeSpec
  record: SubstrateRecord
  requestKind: KindInfo
  messages: ReadonlyMap<string, SubstrateRecord>
  block: ConversationBlock
}) {
  const page = useQuery(recordsQueryOptions(heldParams(compose)))
  const held = useMemo(
    () => heldFor(compose, record, page.data?.records ?? []),
    [compose, record, page.data]
  )
  if (!held.length) return null
  return (
    <div
      role="group"
      aria-label="Waiting for you"
      data-slot="conversation-held"
      className="mt-4 flex max-w-[720px] flex-col gap-1.5"
    >
      <h4 className="text-[13px] font-medium">Waiting for you</h4>
      <p className="text-[12.5px] text-muted-foreground">
        Asked by an agent and held by a rule until you decide. None of these
        happens until you add it.
      </p>
      <ul className="flex flex-col divide-y rounded-[10px] border border-warning/40">
        {held.map(({ request, properties }) => (
          <HeldRow
            key={request.id}
            request={request}
            properties={properties}
            compose={compose}
            requestKind={requestKind}
            messages={messages}
            block={block}
          />
        ))}
      </ul>
    </div>
  )
}

function HeldRow({
  request,
  properties,
  compose,
  requestKind,
  messages,
  block,
}: {
  request: SubstrateRecord
  properties: Record<string, unknown>
  compose: ComposeSpec
  requestKind: KindInfo
  messages: ReadonlyMap<string, SubstrateRecord>
  block: ConversationBlock
}) {
  // The single read carries propertyMeta, which names who proposed it.
  const single = useQuery(changeRequestQueryOptions(request.id))
  const current = single.data ?? request
  const threadId = requestThreadId(current)
  const thread = useQuery({
    ...recordQueryOptions(
      CORE_AUTHORITY,
      LLM_PACKAGE_NAME,
      "thread",
      threadId ?? ""
    ),
    enabled: Boolean(threadId),
  })
  const agent = thread.data ? threadAgentId(thread.data) : undefined
  const proposer = proposerOf(current)
  const actor = agent ? agentActor(agent) : proposer
  const rationale = rationaleOf(current)
  return (
    <li
      data-slot="conversation-held-request"
      className="flex flex-col gap-1.5 px-3 py-2.5 text-[13px]"
    >
      <span className="flex flex-wrap items-center gap-x-1.5 gap-y-0.5">
        {agent ? (
          <AgentRef id={agent} link />
        ) : proposer ? (
          <ActorRef actor={proposer} inlineId={false} />
        ) : (
          <span>An agent</span>
        )}
        <span className="text-muted-foreground">asks to</span>
        <span className="font-medium">
          {lowerFirst(verbLabel(compose, requestKind, properties))}
        </span>
        <RequestSummary
          compose={compose}
          properties={properties}
          messages={messages}
          block={block}
        />
      </span>
      {actor && (
        <span className="text-[12px] text-faint">
          <span>Who: </span>
          <IdText value={actor} />
        </span>
      )}
      {rationale && (
        <p className="border-l-2 pl-2.5 text-[12.5px] [overflow-wrap:anywhere] text-muted-foreground">
          {rationale}
        </p>
      )}
      <DecisionButtons
        request={current}
        op="create"
        review={
          <Button
            size="sm"
            variant="outline"
            render={
              <Link to="/change-requests/$id" params={{ id: request.id }} />
            }
            nativeButton={false}
          >
            Review
          </Button>
        }
      />
    </li>
  )
}
