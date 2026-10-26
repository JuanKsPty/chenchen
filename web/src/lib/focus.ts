import { useMemo, useRef } from 'react'

/**
 * Focus back where it was when a dialog or sheet closes, for one opened from code
 * rather than from a <DialogTrigger>.
 *
 * Radix only returns focus to its own trigger; a dialog opened by state (useImport,
 * the assistant sheet, Categorizar from a row) otherwise drops focus on <body>, and
 * a keyboard user starts again from the top of the page. Spread the result on the
 * content:
 *
 *   const returnFocus = useReturnFocus()
 *   <DialogContent {...returnFocus}>   (or <SheetContent>)
 *
 * It remembers the focused element as the dialog opens and focuses it again on
 * close, if it is still on the page; otherwise Radix's default applies. A dialog
 * opened from a menu item (Renombrar in an account's "Más opciones") remembers the
 * button that opened the menu instead: the item unmounts as the menu closes.
 *
 * rememberFocus() is the same choice on its own, for a provider that opens its
 * dialog from a function (useImport().openImport).
 */
export function useReturnFocus() {
  const previous = useRef<HTMLElement | null>(null)

  return useMemo(
    () => ({
      onOpenAutoFocus: () => {
        previous.current = rememberFocus()
      },
      onCloseAutoFocus: (event: Event) => {
        const element = previous.current
        previous.current = null
        if (element?.isConnected) {
          event.preventDefault()
          element.focus()
        }
      },
    }),
    [],
  )
}

/**
 * What to give focus back to later: the focused element, or, when that is inside a
 * menu, the button that opened the menu. Null when nothing has focus.
 */
export function rememberFocus(): HTMLElement | null {
  const active = document.activeElement
  if (!(active instanceof HTMLElement) || active === document.body) return null
  return menuTrigger(active) ?? active
}

/**
 * The button that opened the menu `element` sits in. Radix names the menu by its
 * trigger (aria-labelledby), which holds even once the menu has started to close;
 * the trigger's aria-controls only while it is open.
 */
function menuTrigger(element: HTMLElement): HTMLElement | null {
  const menu = element.closest<HTMLElement>('[role=menu]')
  if (!menu) return null
  const labelledBy = menu.getAttribute('aria-labelledby')
  const byLabel = labelledBy ? document.getElementById(labelledBy) : null
  if (byLabel) return byLabel
  return menu.id
    ? document.querySelector<HTMLElement>(`[aria-controls="${CSS.escape(menu.id)}"]`)
    : null
}
