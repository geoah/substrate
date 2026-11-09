import {
  AccessBanner,
  BarButton,
  LargeTitle,
  LinkStatus,
  NavBar,
  RefusedBanner,
  cx,
  useLinkedStatus,
} from "@substrate/react"
import { Settings } from "lucide-react"
import type { ReactNode } from "react"
import { useMode } from "../data/mode"
import { go } from "../route"
import { useShell } from "./shell"

/** A screen's frame: the bar (Back, the actions, Settings on a phone), the
 * large title, the link's state and the kit's banners, then the content at
 * the screen's width. `wide` is for the card grids, which take what the
 * window gives them. */
export function Screen(props: {
  title: ReactNode
  /** A line under the title. */
  subtitle?: ReactNode
  back?: { label: string; onClick: () => void }
  actions?: ReactNode
  wide?: boolean
  /** Settings itself shows no Settings button. */
  noSettings?: boolean
  children: ReactNode
}) {
  const { demo } = useMode()
  const shell = useShell()
  const settings = !shell.wide && !props.noSettings && (
    <BarButton aria-label="Settings" onClick={() => go({ screen: "settings" })}>
      <Settings className="size-5" aria-hidden />
    </BarButton>
  )
  return (
    <div
      className={cx(
        "mx-auto w-full",
        props.wide ? "max-w-[1600px]" : "max-w-3xl",
        shell.wide ? "pb-16" : "pb-32"
      )}
    >
      <NavBar
        onBack={props.back?.onClick}
        backLabel={props.back?.label}
        actions={
          (props.actions || settings) && (
            <>
              {props.actions}
              {settings}
            </>
          )
        }
      />
      <LargeTitle>{props.title}</LargeTitle>
      {props.subtitle && (
        <p className="-mt-2 px-4 pb-3 text-[15px] text-secondary">
          {props.subtitle}
        </p>
      )}
      {demo ? <DemoBanner /> : <LiveBanners />}
      <div className="px-4">{props.children}</div>
    </div>
  )
}

function LiveBanners() {
  const link = useLinkedStatus()
  return (
    <>
      <LinkStatus status={link.status} words={link.words} />
      <AccessBanner className="mx-4 mb-4" />
      <RefusedBanner
        className="mx-4 mb-4"
        onOpenSettings={() => go({ screen: "settings" })}
      />
    </>
  )
}

function DemoBanner() {
  return (
    <p
      role="status"
      className="mx-4 mb-4 rounded-[10px] bg-fill px-3 py-2 text-[13px] text-secondary"
    >
      A demo: an invented person's data, made in this browser. Nothing is read
      from or written to a substrate.
    </p>
  )
}
