/** What a screen not built yet shows. */
export function Placeholder(props: { what: string }) {
  return (
    <p className="py-16 text-center text-[15px] text-secondary">{props.what}</p>
  )
}
