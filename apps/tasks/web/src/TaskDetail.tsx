import {
  ActionRow,
  BarButton,
  NavBar,
  Row,
  Section,
  SelectRow,
  cx,
} from "@substrate/react"
import { Link2 } from "lucide-react"
import { usePending, useTaskActions } from "./actions"
import { ApprovalNote } from "./CollectionsScreen"
import { DescriptionSection, DetailsSection, statusText } from "./Details"
import { safeURL } from "./markdown"
import {
  dueWords,
  fieldLabel,
  fullWords,
  inboxTitle,
  listTitle,
  priorities,
  statusWords,
  taskTitle,
  type Priority,
  type TaskItem,
} from "./model"
import { nameWords, propertyLabel } from "./sources"
import type { TasksModel } from "./state"
import { StatusMark } from "./TaskRow"
import { words } from "./words"

const noGroup = "__none__"

/** The open task: its name beside its round mark, then each fact once under
 * the label the editor uses, the group and the priority as the choices that
 * change them where they stand (where its collection is writable), the
 * notes and the link as sections of their own, then what can be done to
 * it, Delete apart. Edit is the bar's, for the app's own tasks. Another
 * collection's task also lists its other properties under Details. */
export function TaskDetail(props: {
  task: TaskItem
  model: TasksModel
  now: Date
  onBack?: () => void
  /** Beside the list, whose large title is the page's heading. */
  beside?: boolean
}) {
  const { task: t, now, model } = props
  const Title = props.beside ? "h2" : "h1"
  const actions = useTaskActions()
  const decision = usePending().get(t.id)
  const pending = decision?.action === "complete" ? decision.until : undefined
  const due = t.due ? dueWords(t.due, now) : undefined
  const title = taskTitle(t)
  const open = t.status === "open"
  const link = t.url ? safeURL(t.url) : undefined
  const c = t.item.collection
  const a = t.item.actions
  const writable = !c.viewOnly
  const definition = c.declaration.definition
  const toggle =
    pending !== undefined ? "Undo" : open ? "Mark as Done" : "Reopen"
  const groups = c.group
    ? [...model.groups.values()].filter((l) =>
        l.id.startsWith(`${c.group!.kind}/`)
      )
    : []
  const groupLabel = c.group
    ? t.own
      ? fieldLabel.list
      : propertyLabel(definition, c.group.property)
    : undefined
  const noneLabel = c.group
    ? t.own
      ? inboxTitle
      : words.lists.none(propertyLabel(definition, c.group.property))
    : ""
  const groupValue =
    t.item.group === "missing" ? "__missing__" : (t.list ?? noGroup)
  const status =
    decision && decision.action !== "complete"
      ? `${words.undo[decision.action]}, tap Undo to keep it open`
      : pending !== undefined
        ? "Done, tap Undo to keep it open"
        : t.own
          ? statusWords[t.status]
          : (statusText(t) ?? statusWords[t.status])

  return (
    <div className="mx-auto w-full max-w-2xl pb-32">
      <NavBar
        onBack={props.onBack}
        actions={
          t.own &&
          writable && (
            <BarButton onClick={() => actions.edit(t.id)} title="Edit (E)">
              Edit
            </BarButton>
          )
        }
      />
      <div className="px-4 pt-2">
        <ApprovalNote collection={c} />
        <Section>
          <div className="flex items-center gap-2 py-1.5 pr-1.5 pl-4">
            <Title
              className={cx(
                "min-w-0 flex-1 text-[22px] leading-snug font-semibold break-words select-text",
                open && pending === undefined ? "text-label" : "text-secondary",
                (t.status === "abandoned" || pending !== undefined) &&
                  "line-through"
              )}
            >
              {title}
            </Title>
            <StatusMark
              status={t.status}
              pending={pending}
              label={toggle}
              disabled={
                pending === undefined &&
                !(open ? a.complete.length : a.reopen.length)
              }
              onToggle={() =>
                open && pending === undefined
                  ? actions.complete(t.id)
                  : actions.reopen(t.id)
              }
            />
          </div>
        </Section>
        <Section>
          <Row label={fieldLabel.status} value={status} />
          {c.waitingGroup && (
            <Row label={c.waitingGroup.label} value={words.group.outOfReach} />
          )}
          {c.group &&
            (writable ? (
              <SelectRow
                label={groupLabel}
                value={groupValue}
                options={[
                  ...(t.item.group === "missing"
                    ? [{ value: "__missing__", label: words.lists.missing }]
                    : []),
                  { value: noGroup, label: noneLabel },
                  ...groups.map((l) => ({ value: l.id, label: listTitle(l) })),
                ]}
                onChange={(v) =>
                  v !== "__missing__" &&
                  actions.move(t.id, v === noGroup ? undefined : v)
                }
              />
            ) : (
              <Row
                label={groupLabel}
                value={
                  t.item.group === "missing"
                    ? words.lists.missing
                    : t.list
                      ? listTitle(model.list(t.list) ?? { name: "" })
                      : noneLabel
                }
              />
            ))}
          {(t.own || c.due || c.dueDay) && (
            <Row
              label={fieldLabel.due}
              value={t.due ? fullWords(t.due) : "No due date"}
              valueClassName={
                due?.tone === "overdue" && open ? "text-danger!" : "text-label!"
              }
            />
          )}
          {t.own ? (
            <SelectRow<Priority>
              label={fieldLabel.priority}
              value={t.priority}
              options={priorities}
              onChange={(p) => actions.setPriority(t.id, p)}
            />
          ) : (
            c.priority &&
            (writable ? (
              <SelectRow
                label={propertyLabel(definition, c.priority)}
                value={t.item.priority ?? noGroup}
                options={[
                  { value: noGroup, label: words.collection.none },
                  ...c.priorities.map((v) => ({
                    value: v,
                    label: nameWords(v),
                  })),
                ]}
                onChange={(v) =>
                  actions.setPriorityValue(t.id, v === noGroup ? null : v)
                }
              />
            ) : (
              <Row
                label={propertyLabel(definition, c.priority)}
                value={t.priorityLabel ?? words.collection.none}
              />
            ))
          )}
          {t.status === "done" && t.completedAt && (
            <Row
              label={fieldLabel.completed}
              value={fullWords({ day: "", at: t.completedAt })}
              valueClassName="text-label!"
            />
          )}
        </Section>
        <DescriptionSection
          task={t}
          label={t.own ? fieldLabel.notes : undefined}
        />
        {t.url && (
          <Section header={fieldLabel.link}>
            {link ? (
              <a
                href={link}
                target="_blank"
                rel="noreferrer noopener"
                className="flex min-h-11 items-center gap-3 px-4 py-2.5 text-[17px] text-accent-text"
              >
                <Link2 className="size-5 shrink-0" />
                <span className="line-clamp-2 break-all">{t.url}</span>
              </a>
            ) : (
              <p className="px-4 py-2.5 text-[17px] break-all">{t.url}</p>
            )}
          </Section>
        )}
        {!t.own && <DetailsSection task={t} model={model} />}
        {(decision ||
          a.complete.length > 0 ||
          a.wontDo.length > 0 ||
          a.reopen.length > 0) && (
          <Section>
            {decision ? (
              <ActionRow onClick={() => actions.reopen(t.id)}>
                {words.undo.action}
              </ActionRow>
            ) : open ? (
              <>
                {a.complete.length > 0 && (
                  <ActionRow onClick={() => actions.complete(t.id)}>
                    Mark as Done
                  </ActionRow>
                )}
                {a.wontDo.length > 0 && (
                  <ActionRow onClick={() => actions.abandon(t.id)}>
                    Won't Do
                  </ActionRow>
                )}
              </>
            ) : (
              <ActionRow onClick={() => actions.reopen(t.id)}>Reopen</ActionRow>
            )}
          </Section>
        )}
        {writable && (
          <Section>
            <ActionRow destructive onClick={() => actions.deleteTask(t.id)}>
              Delete Task
            </ActionRow>
          </Section>
        )}
      </div>
    </div>
  )
}
