/** Answering a question in your own words (decision 0327). The question's
 * card is drawn deep in the transcript and the composer belongs to the
 * conversation, so the conversation offers this to its cards: a card asks
 * to be answered in a message, the composer takes the cursor and says which
 * question the next message answers, and that message carries the
 * question's id. Outside a conversation, or against a server that does not
 * take the field, nothing is offered and the card has no such button. */

import { createContext, useContext } from "react"

export interface QuestionReply {
  /** The question the next message answers, when one is chosen. */
  replyingTo?: string
  /** Answer `id` with the next message; `prompt` is what the composer
   * shows it is answering. */
  begin: (id: string, prompt: string) => void
  /** Go back to an ordinary message. */
  cancel: () => void
}

export const QuestionReplyContext = createContext<QuestionReply | null>(null)

/** The conversation's offer, or null where a question is answered by its
 * options alone. */
export function useQuestionReply(): QuestionReply | null {
  return useContext(QuestionReplyContext)
}
