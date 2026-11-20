import type { ReactNode } from "react"
import { SCORE_SOURCES } from "../../healthspan/content"
import type { Explainer } from "../../healthspan/explainer"
import { InfoSources } from "../Info"

// "How the age score works": what the score is and is not, the function,
// its windows and rules, each contributor with its target, the sources
// that feed it, and what this app does differently on purpose. Every
// number comes from healthspan/explainer.ts, which reads the engine's
// exports; nothing here is typed in.

/** "Hours of sleep" to "hours of sleep" mid-sentence; "VO2 max" stays. */
const lower = (s: string) =>
  /^[A-Z][a-z]/.test(s) ? s[0].toLowerCase() + s.slice(1) : s

/** The changes from the band-only model, as the plan names them. */
export const CHANGES_ON_PURPOSE: { title: string; body: string }[] = [
  {
    title: "A sleep's night comes from its own end",
    body: "A sleep belongs to the night before the day it ended on, not to the band's cycle. A sleep that ends after the next cycle has started, such as a late nap or a second sleep after waking, moves to the night it ended in, and the resting heart rate measured over it moves with it: that night's resting heart rate, its place in the 21-in-31 gate and its day worn.",
  },
  {
    title: "Every source's strength workouts count",
    body: "Strength time counts workouts any source calls strength training, pilates or yoga, as well as the white paper's own list of sports by the name a source gives them, so a phone's traditional or functional strength training counts.",
  },
  {
    title: "Sleep consistency is the source's own score",
    body: "Where none of your sleep sources scores consistency, there is no consistency contributor: it is not worked out some other way, and it counts as 0 years.",
  },
]

function Block(props: { title: string; children: ReactNode }) {
  return (
    <section className="mb-7">
      <h2 className="mb-1.5 px-4 text-[13px] tracking-wide text-secondary uppercase">
        {props.title}
      </h2>
      <div className="rounded-[10px] bg-card px-4 py-3 text-[15px] leading-snug">
        {props.children}
      </div>
    </section>
  )
}

/** A label over its value, a list of them. */
function Facts(props: { items: { key: string; value: ReactNode }[] }) {
  return (
    <dl className="flex flex-col gap-3">
      {props.items.map((i) => (
        <div key={i.key}>
          <dt className="text-[13px] text-secondary">{i.key}</dt>
          <dd>{i.value}</dd>
        </div>
      ))}
    </dl>
  )
}

export function HowItWorks({ explainer: e }: { explainer: Explainer }) {
  return (
    <>
      <Block title="What the score is">
        <p>
          The age score is an estimate this app works out in your browser from
          published reference curves: each contributor's average is set against
          a target, and the mortality hazard the research ties to the gap is
          turned into years. It is not a diagnosis, and it is not any vendor's
          product or number; your device maker's own score, where it shows one,
          may differ.
        </p>
        <div className="mt-3">
          <InfoSources
            label="The curves and targets come from"
            sources={Object.values(SCORE_SOURCES)}
          />
        </div>
      </Block>
      <Block title="The function">
        <p>{e.function}</p>
      </Block>
      <Block title="Windows and rules">
        <Facts items={e.rules} />
      </Block>
      <section className="mb-7">
        <h2 className="mb-1.5 px-4 text-[13px] tracking-wide text-secondary uppercase">
          Contributors
        </h2>
        <ul className="overflow-hidden rounded-[10px] bg-card">
          {e.rows.map((r, i) => (
            <li
              key={r.id}
              className={`px-4 py-3 ${i ? "border-t border-separator/60" : ""}`}
            >
              <p className="text-[17px] font-semibold">{r.label}</p>
              {r.target ? (
                <dl className="mt-1 flex flex-wrap gap-x-6 gap-y-1">
                  <div>
                    <dt className="text-[13px] text-secondary">Target</dt>
                    <dd className="text-[15px]">
                      <span className="tabular-nums">{r.target}</span>
                      {r.targetNote && (
                        <span className="text-secondary"> {r.targetNote}</span>
                      )}
                    </dd>
                  </div>
                  <div>
                    <dt className="text-[13px] text-secondary">Adds at most</dt>
                    <dd className="text-[15px] tabular-nums">{r.addsAtMost}</dd>
                  </div>
                  <div>
                    <dt className="text-[13px] text-secondary">
                      Removes at most
                    </dt>
                    <dd className="text-[15px] tabular-nums">
                      {r.removesAtMost}
                    </dd>
                  </div>
                </dl>
              ) : (
                <p className="mt-1 text-[15px] text-secondary">
                  The target needs your birth date and sex.
                </p>
              )}
              <p className="mt-2 text-[13px] text-secondary">Feeds from</p>
              <p className="text-[15px] leading-snug">{r.feedsFrom}</p>
            </li>
          ))}
        </ul>
      </section>
      <section className="mb-7">
        <h2 className="mb-1.5 px-4 text-[13px] tracking-wide text-secondary uppercase">
          Sources
        </h2>
        <ul className="overflow-hidden rounded-[10px] bg-card">
          {e.sources.map((s, i) => (
            <li
              key={s.data}
              className={`px-4 py-3 ${i ? "border-t border-separator/60" : ""}`}
            >
              <p className="text-[17px] font-semibold">{s.data}</p>
              <p className="mt-1 text-[15px] leading-snug">{s.ranking}</p>
              <p className="mt-2 text-[13px] text-secondary">Read from</p>
              <p className="text-[15px]">
                {s.order.length ? s.order.join(", then ") : "No source yet"}
              </p>
              <p className="mt-2 text-[13px] text-secondary">Feeds</p>
              <p className="text-[15px]">{s.feeds.map(lower).join(", ")}</p>
            </li>
          ))}
        </ul>
        <p className="mt-1.5 px-4 text-[13px] text-secondary">
          The sources are your repository's own; rank them on the Sources
          screen.
        </p>
      </section>
      <section className="mb-7">
        <h2 className="mb-1.5 px-4 text-[13px] tracking-wide text-secondary uppercase">
          What changes on purpose
        </h2>
        <ul className="overflow-hidden rounded-[10px] bg-card">
          {CHANGES_ON_PURPOSE.map((c, i) => (
            <li
              key={c.title}
              className={`px-4 py-3 ${i ? "border-t border-separator/60" : ""}`}
            >
              <p className="text-[17px] font-semibold">{c.title}</p>
              <p className="mt-1 text-[15px] leading-snug">{c.body}</p>
            </li>
          ))}
        </ul>
        <p className="mt-1.5 px-4 text-[13px] text-secondary">
          Measured against the model the curves were fitted to, these are the
          only changes; everything else keeps its arithmetic.
        </p>
      </section>
    </>
  )
}
