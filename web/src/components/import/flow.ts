/**
 * What the two import dialogs share (ImportStatementDialog, UploadReviewDialog): a
 * flow that starts again on every opening unless something it started still runs,
 * and focus that follows the view on screen.
 */
import { useEffect, useLayoutEffect, useRef, useState } from 'react'

// A view change replaces the button that had the focus: move it to the new view's
// title, so a screen reader hears what is there now.
export const DIALOG_TITLE = '[role="dialog"] [data-slot="dialog-title"]'

/**
 * A new round on every opening: key the flow with `round` and it remounts and starts
 * clean, while a closing dialog keeps showing what it showed as it fades out. While
 * the flow says it is busy (setBusy), an opening keeps its round instead: reopened,
 * the dialog shows that work finishing (and how it went), not a fresh flow that could
 * start more on top of it. `isOpen` is whether it is open now, for a callback that
 * lands after closing (a toast instead of the dialog's own message).
 */
export function useDialogRound(open: boolean) {
  const [round, setRound] = useState(0)
  const [busy, setBusy] = useState(false)
  const [wasOpen, setWasOpen] = useState(open)
  if (open !== wasOpen) {
    setWasOpen(open)
    if (open && !busy) setRound((n) => n + 1)
  }
  const isOpen = useRef(open)
  useEffect(() => {
    isOpen.current = open
  }, [open])
  return { round, isOpen, setBusy }
}

/**
 * Focus for a flow whose views replace each other. `shown` is the view on screen;
 * focusOn(view, selector) asks for the element to be focused once that view is in
 * the DOM, in the same commit, and only then (never the old view's title).
 */
export function useViewFocus<V extends string>(shown: V) {
  const pending = useRef<{ view: V; selector: string } | null>(null)
  useLayoutEffect(() => {
    const request = pending.current
    if (!request || request.view !== shown) return
    pending.current = null
    document.querySelector<HTMLElement>(request.selector)?.focus()
  })
  return (view: V, selector: string) => {
    pending.current = { view, selector }
  }
}
