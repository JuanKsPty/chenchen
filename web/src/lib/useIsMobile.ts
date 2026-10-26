import { useEffect, useState } from 'react'

/**
 * True below `md` (768px): a phone. This is the question "is this device a
 * phone?" (whether to offer the PIN pad, or to trust this device for a PIN in
 * Ajustes), not "which chrome is showing". The shell's chrome switches at lg
 * (1024px) instead; for that use `useMediaQuery(DESKTOP_QUERY)` from
 * `@/components/shell/media`, or better, the `lg:` prefix in CSS.
 *
 * Initialised synchronously from `matchMedia` rather than starting `false`
 * and correcting itself after mount: a naive check reports `false` on the
 * first render even on an actual phone. A page that decides once whether to
 * even ask `/api/auth/device` (SignInPage) cannot correct a wrong first guess
 * the way toggling a class can.
 */
const QUERY = '(max-width: 767px)'

export function useIsMobile(): boolean {
  const [isMobile, setIsMobile] = useState(() => window.matchMedia(QUERY).matches)

  useEffect(() => {
    const mql = window.matchMedia(QUERY)
    const onChange = () => setIsMobile(mql.matches)
    mql.addEventListener('change', onChange)
    return () => mql.removeEventListener('change', onChange)
  }, [])

  return isMobile
}
