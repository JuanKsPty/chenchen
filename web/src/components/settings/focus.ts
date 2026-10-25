/**
 * focusWhenReady(get)  moves focus to `get()` as soon as that element is on the page
 *                      and enabled, trying once a frame for about half a second.
 *
 * Ajustes and Reglas swap what is on screen after a request (the PIN form closes,
 * "Quitar PIN" goes away, a rule's row leaves the list) and disable buttons while
 * one runs, and a focused element that disappears or is disabled drops focus on
 * <body>. This puts it somewhere that makes sense instead: the field to fix, the
 * button that comes back, the next row.
 *
 * It gives up if focus has meanwhile moved somewhere on purpose, so it never steals
 * focus from the person. Not on purpose: still where it was when called, <body>, or
 * a container of the target (a dialog pulls focus onto itself when the focused
 * field is disabled during a request).
 */
export function focusWhenReady(get: () => HTMLElement | null | undefined, frames = 30) {
  const from = document.activeElement

  const attempt = (left: number) => {
    const element = get()
    const active = document.activeElement
    const moved =
      active &&
      active !== from &&
      active !== document.body &&
      !(element && active.contains(element))
    if (moved) return
    if (element?.isConnected && !element.matches(':disabled')) {
      element.focus()
      return
    }
    if (left > 0) requestAnimationFrame(() => attempt(left - 1))
  }

  requestAnimationFrame(() => attempt(frames))
}
