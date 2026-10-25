import { useCallback, useSyncExternalStore } from 'react'

/**
 * The shell's two widths, as media queries, for the few decisions CSS cannot make
 * (which assistant surface to mount, where toasts sit). Everything visual switches
 * with the matching Tailwind prefix instead.
 *
 *   DESKTOP_QUERY  `lg` (1024px): the desktop top bar and the assistant panel; below
 *                  it, the mobile top bar, the tab bar and the assistant sheet. Not
 *                  md: at the spec's sizes the desktop bar needs about 830px, so a
 *                  768px tablet gets the phone's chrome.
 *   PUSH_QUERY     `xl` (1280px): the open assistant panel pushes the page aside
 *                  instead of floating over it on the scrim.
 */
export const DESKTOP_QUERY = '(min-width: 1024px)'
export const PUSH_QUERY = '(min-width: 1280px)'

/** Whether `query` matches, read synchronously on the first render and kept live. */
export function useMediaQuery(query: string): boolean {
  const subscribe = useCallback(
    (onChange: () => void) => {
      const list = window.matchMedia(query)
      list.addEventListener('change', onChange)
      return () => list.removeEventListener('change', onChange)
    },
    [query],
  )
  return useSyncExternalStore(subscribe, () => window.matchMedia(query).matches)
}

/** Whether the page has scrolled at all: the bars gain their shadow then. */
export function useScrolled(): boolean {
  return useSyncExternalStore(subscribeScroll, () => window.scrollY > 0)
}

function subscribeScroll(onChange: () => void) {
  window.addEventListener('scroll', onChange, { passive: true })
  return () => window.removeEventListener('scroll', onChange)
}
