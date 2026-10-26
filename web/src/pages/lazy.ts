/**
 * The signed-in half of the app, loaded on demand.
 *
 * The portada, Entrar and Registro ship in the first chunk and nothing else does:
 * a visitor who never signs in never downloads the shell, the assistant or the
 * pages. Signed in, the shell arrives as one chunk (SignedInApp) and each page as
 * its own.
 *
 *   loadSignedInApp(pathname)  the shell's chunk, fetched together with the page
 *                              for `pathname`, so the first screen after "Abriendo
 *                              tu chenchén…" is the whole page, never its fallback
 *   preloadSignedInApp()       the same, early: on the way in (Entrar, Registro) or
 *                              while a session restores on a signed-in address
 *   preloadAllPages()          every page, once the shell is up and the browser is
 *                              idle, so moving between sections never waits
 *   HomePage … SettingsPage    React.lazy components for SignedInApp's routes
 *
 * A chunk that fails to load (a deployment replaced it) throws inside the shell's
 * ErrorBoundary, whose "Recargar" fetches the new build.
 */
import { lazy } from 'react'
import { matchPath } from 'react-router-dom'

import {
  ACCOUNTS_PATH,
  HOME_PATH,
  MOVEMENTS_PATH,
  RULES_PATH,
  SETTINGS_PATH,
  SPENDING_PATH,
} from '@/lib/nav'

const load = {
  home: () => import('./HomePage'),
  accounts: () => import('./AccountsPage'),
  account: () => import('./AccountPage'),
  movements: () => import('./MovementsPage'),
  spending: () => import('./SpendingPage'),
  rules: () => import('./RulesPage'),
  settings: () => import('./SettingsPage'),
}

export const HomePage = lazy(() => load.home().then((m) => ({ default: m.HomePage })))
export const AccountsPage = lazy(() =>
  load.accounts().then((m) => ({ default: m.AccountsPage })),
)
export const AccountPage = lazy(() =>
  load.account().then((m) => ({ default: m.AccountPage })),
)
export const MovementsPage = lazy(() =>
  load.movements().then((m) => ({ default: m.MovementsPage })),
)
export const SpendingPage = lazy(() =>
  load.spending().then((m) => ({ default: m.SpendingPage })),
)
export const RulesPage = lazy(() => load.rules().then((m) => ({ default: m.RulesPage })))
export const SettingsPage = lazy(() =>
  load.settings().then((m) => ({ default: m.SettingsPage })),
)

/** Which page a signed-in address opens; old addresses land on their new page. */
const ROUTES: [pattern: string, loader: () => Promise<unknown>][] = [
  [HOME_PATH, load.home],
  ['/', load.home],
  ['/entrar', load.home],
  ['/registro', load.home],
  ['/panel', load.home],
  [ACCOUNTS_PATH, load.accounts],
  [`${ACCOUNTS_PATH}/:id`, load.account],
  [MOVEMENTS_PATH, load.movements],
  ['/historial', load.movements],
  [SPENDING_PATH, load.spending],
  [RULES_PATH, load.rules],
  ['/reglas', load.rules],
  [SETTINGS_PATH, load.settings],
  ['/seguridad', load.settings],
]

const quietly = (promise: Promise<unknown>) => promise.catch(() => undefined)

export function loadSignedInApp(pathname: string) {
  const page = ROUTES.find(([pattern]) => matchPath(pattern, pathname))?.[1]
  // The page is a head start only: if it fails here, its own lazy() tries again
  // and reports the failure where the page would be.
  return Promise.all([import('@/SignedInApp'), page && quietly(page())]).then(
    ([shell]) => shell,
  )
}

export function preloadSignedInApp(pathname = '/inicio') {
  void quietly(loadSignedInApp(pathname))
}

export function preloadAllPages() {
  for (const loader of Object.values(load)) void quietly(loader())
}

/** Runs `task` once the browser is idle (or after a short wait where it cannot say). */
export function whenIdle(task: () => void): () => void {
  if (typeof window.requestIdleCallback === 'function') {
    const handle = window.requestIdleCallback(task, { timeout: 3000 })
    return () => window.cancelIdleCallback(handle)
  }
  const handle = window.setTimeout(task, 1500)
  return () => window.clearTimeout(handle)
}
