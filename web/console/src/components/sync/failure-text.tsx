import { splitFailure } from "@/lib/sync"

/** A sync failure as the owner reads it: the sentence on its first line, and
 * the provider's or the library's own text after it folded under Technical
 * details, never run into the sentence. */
export function FailureText({ text }: { text: string }) {
  const { line, technical } = splitFailure(text)
  return (
    <>
      <span className="break-words">{line}</span>
      {technical && (
        <details className="text-xs text-muted-foreground">
          <summary className="cursor-pointer select-none">
            Technical details
          </summary>
          <pre className="mt-1 font-mono break-words whitespace-pre-wrap">
            {technical}
          </pre>
        </details>
      )}
    </>
  )
}
