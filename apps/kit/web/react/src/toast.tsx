import {
  createContext,
  useCallback,
  useContext,
  useRef,
  useState,
  type ReactNode,
} from "react"

interface Toast {
  id: number
  text: string
  tone: "info" | "error"
}

const ToastContext = createContext<
  (text: string, tone?: Toast["tone"]) => void
>(() => {})

/** A short message at the bottom of the screen: a write that failed, or one
 * the server refused, said where the owner is looking. */
export function useToast() {
  return useContext(ToastContext)
}

export function ToastHost({ children }: { children: ReactNode }) {
  const [toasts, setToasts] = useState<Toast[]>([])
  const next = useRef(1)
  const show = useCallback((text: string, tone: Toast["tone"] = "info") => {
    const id = next.current++
    setToasts((t) => [...t, { id, text, tone }])
    setTimeout(
      () => setToasts((t) => t.filter((x) => x.id !== id)),
      tone === "error" ? 7000 : 3500
    )
  }, [])
  return (
    <ToastContext.Provider value={show}>
      {children}
      <div className="pointer-events-none fixed inset-x-0 bottom-24 z-50 flex flex-col items-center gap-2 px-4 lg:bottom-6">
        {toasts.map((t) => (
          <div
            key={t.id}
            role="status"
            className={`pointer-events-auto max-w-md rounded-2xl px-4 py-3 text-[15px] shadow-lg backdrop-blur-xl ${
              t.tone === "error"
                ? "bg-danger text-white"
                : "bg-label/85 text-canvas"
            }`}
          >
            {t.text}
          </div>
        ))}
      </div>
    </ToastContext.Provider>
  )
}
