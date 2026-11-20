import type { MetricInfoData } from "../../labs/view"
import { Badge, StatusDot } from "../Badge"
import { InfoBlock, InfoSources, InfoTitle } from "../Info"

/** A card's info panel: everything is the catalogue's and the newest
 * result's; nothing is written per entry here. */
export function MetricInfo({ info }: { info: MetricInfoData }) {
  return (
    <div className="flex flex-col">
      <InfoTitle
        badge={info.panel ? <Badge label={info.panel} isMono /> : undefined}
      >
        {info.name}
      </InfoTitle>
      {info.description && <InfoBlock>{info.description}</InfoBlock>}
      {info.why && <InfoBlock label="Why it matters">{info.why}</InfoBlock>}
      {info.moves && <InfoBlock label="What moves it">{info.moves}</InfoBlock>}
      {info.direction && <InfoBlock>{info.direction}</InfoBlock>}
      {(info.bands.length > 0 || info.labRange) && (
        <InfoBlock label="Bands">
          <ul className="flex flex-col gap-1">
            {info.bands.map((b) => (
              <li key={b.text} className="flex items-center gap-2">
                <StatusDot level={b.tone} label={b.label} />
                <span>{b.text}</span>
              </li>
            ))}
            {info.labRange && (
              <li className="flex items-center gap-2 text-secondary">
                <span
                  aria-hidden
                  className="border-range-edge bg-range inline-block h-2 w-3 shrink-0 rounded-[2px] border border-dotted"
                />
                <span>{info.labRange}</span>
              </li>
            )}
          </ul>
        </InfoBlock>
      )}
      {info.assessment && <InfoBlock>{info.assessment}</InfoBlock>}
      {info.cadence && <InfoBlock muted>{info.cadence}</InfoBlock>}
      <InfoSources sources={info.sources} />
    </div>
  )
}
