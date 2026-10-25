import { createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react'

/**
 * The appearance preference (Claro, Oscuro or Sistema), owned above the router
 * so the landing page and the sign-in form follow it too. It is a durable
 * choice, set in Ajustes, not view state that resets on navigation.
 *
 * The inline script in `index.html` already applied `.dark` (or not) and the
 * matching `<meta name="theme-color">` before React mounted, so the first
 * render never has to guess. This provider keeps both correct as the choice or
 * the OS preference changes afterwards. The storage key must stay identical
 * to that script's.
 *
 *   const { theme, setTheme, resolvedTheme } = useTheme()
 *   theme          'light' | 'dark' | 'system' (Claro, Oscuro, Sistema; THEME_LABEL)
 *   setTheme(t)    stores it under `chenchen.theme` and applies it at once
 *   resolvedTheme  'light' | 'dark', what is painted right now
 */

export type Theme = 'light' | 'dark' | 'system'

const THEME_CYCLE: Record<Theme, Theme> = {
  light: 'dark',
  dark: 'system',
  system: 'light',
}

/** Claro, then Oscuro, then Sistema, then Claro again: one click, one step
 * forward, for a single toggle button. Ajustes uses `setTheme` directly. */
export function nextTheme(theme: Theme): Theme {
  return THEME_CYCLE[theme]
}

export const THEME_LABEL: Record<Theme, string> = {
  light: 'Claro',
  dark: 'Oscuro',
  system: 'Sistema',
}

interface ThemeContextValue {
  theme: Theme
  setTheme: (value: Theme) => void
  /** What `theme` actually resolves to right now — `system` is never a
   * paint-time answer, callers that need one (sonner's `theme` prop) want
   * this instead. */
  resolvedTheme: 'light' | 'dark'
}

const THEME_KEY = 'chenchen.theme'

/** Reads the stored preference, defaulting to following the OS. */
function readTheme(): Theme {
  try {
    const stored = window.localStorage.getItem(THEME_KEY)
    return stored === 'light' || stored === 'dark' || stored === 'system'
      ? stored
      : 'system'
  } catch {
    // Private browsing, or storage disabled. The preference is not important enough to
    // fail over.
    return 'system'
  }
}

/** Tracks the OS preference live, for "Sistema": switching the whole machine
 * to dark at dusk should not need a second toggle inside the app. */
function useSystemPrefersDark(): boolean {
  const [prefersDark, setPrefersDark] = useState(
    () => window.matchMedia('(prefers-color-scheme: dark)').matches,
  )

  useEffect(() => {
    const media = window.matchMedia('(prefers-color-scheme: dark)')
    const onChange = () => setPrefersDark(media.matches)
    media.addEventListener('change', onChange)
    return () => media.removeEventListener('change', onChange)
  }, [])

  return prefersDark
}

const ThemeContext = createContext<ThemeContextValue | null>(null)

export function useTheme(): ThemeContextValue {
  const value = useContext(ThemeContext)
  if (!value) throw new Error('useTheme must be used inside a ThemeProvider')
  return value
}

export function ThemeProvider({ children }: { children: React.ReactNode }) {
  const [theme, setThemeState] = useState<Theme>(readTheme)
  const systemPrefersDark = useSystemPrefersDark()

  const resolvedTheme: 'light' | 'dark' =
    theme === 'system' ? (systemPrefersDark ? 'dark' : 'light') : theme

  useEffect(() => {
    const root = document.documentElement
    root.classList.toggle('dark', resolvedTheme === 'dark')
    // The browser chrome follows the page background of the mode in use, read
    // from the token so the colour lives in styles.css only.
    const background = getComputedStyle(root).getPropertyValue('--background').trim()
    if (background) {
      document
        .querySelector('meta[name="theme-color"]')
        ?.setAttribute('content', background)
    }
  }, [resolvedTheme])

  const setTheme = useCallback((value: Theme) => {
    setThemeState(value)
    try {
      window.localStorage.setItem(THEME_KEY, value)
    } catch {
      // Nothing to do: the choice still applies to this visit, it just will
      // not be remembered for the next one.
    }
  }, [])

  const value = useMemo<ThemeContextValue>(
    () => ({ theme, setTheme, resolvedTheme }),
    [theme, setTheme, resolvedTheme],
  )

  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>
}
