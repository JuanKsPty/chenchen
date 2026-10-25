/**
 * Everything behind the sign-in: the shell and its routes. Its own chunk, loaded by
 * App through pages/lazy (loadSignedInApp), and each page inside it is another.
 * While a page's chunk is on its way, the shell stays and <main> shows the page
 * fallback (shell/PageFallback); once the shell is up, every other page is fetched
 * in the background so moving between sections does not wait for the network.
 */
import { useEffect } from 'react'
import { Navigate, Route, Routes, useLocation } from 'react-router-dom'

import { AppShell } from '@/components/shell/AppShell'
import {
  ACCOUNTS_PATH,
  HOME_PATH,
  MOVEMENTS_PATH,
  RULES_PATH,
  SECURITY_HREF,
  SETTINGS_PATH,
  SPENDING_PATH,
} from '@/lib/nav'
import {
  AccountPage,
  AccountsPage,
  HomePage,
  MovementsPage,
  preloadAllPages,
  RulesPage,
  SettingsPage,
  SpendingPage,
  whenIdle,
} from '@/pages/lazy'
import { NotFoundPage } from '@/pages/NotFoundPage'

export default function SignedInApp() {
  useEffect(() => whenIdle(preloadAllPages), [])

  return (
    <AppShell>
      <Routes>
        {/* Inicio lives at /inicio rather than at /, leaving the root to the public
            portada: a route that meant one thing or the other depending on a cookie
            could not be linked to. */}
        <Route path={HOME_PATH} element={<HomePage />} />
        <Route path={ACCOUNTS_PATH} element={<AccountsPage />} />
        <Route path={`${ACCOUNTS_PATH}/:id`} element={<AccountPage />} />
        <Route path={MOVEMENTS_PATH} element={<MovementsPage />} />
        <Route path={SPENDING_PATH} element={<SpendingPage />} />
        <Route path={RULES_PATH} element={<RulesPage />} />
        <Route path={SETTINGS_PATH} element={<SettingsPage />} />

        {/* Somebody who signs in from a form, or opens the portada with a session,
            lands on Inicio rather than on a page they no longer need, or back where
            they were headed when the session ended. */}
        <Route path="/" element={<Navigate to={HOME_PATH} replace />} />
        <Route path="/entrar" element={<RedirectToReturnTo />} />
        <Route path="/registro" element={<RedirectToReturnTo />} />

        {/* Old addresses, from bookmarks and from before the redesign. */}
        <Route path="/panel" element={<Navigate to={HOME_PATH} replace />} />
        <Route path="/historial" element={<RedirectKeepingQuery to={MOVEMENTS_PATH} />} />
        <Route path="/reglas" element={<Navigate to={RULES_PATH} replace />} />
        <Route path="/seguridad" element={<Navigate to={SECURITY_HREF} replace />} />

        <Route path="*" element={<NotFoundPage />} />
      </Routes>
    </AppShell>
  )
}

/**
 * The sign-in form's way out. The session can flip to signed-in before the form's
 * own navigate lands, so this route renders at /entrar first: it has to honour the
 * address App's RedirectToSignIn remembered, or that return would be lost.
 */
function RedirectToReturnTo() {
  const { state } = useLocation()
  const from = (state as { from?: string } | null)?.from
  const to = from?.startsWith('/') && !from.startsWith('//') ? from : HOME_PATH
  return <Navigate to={to} replace />
}

/** A redirect that carries the query string along (filters in an old link). */
function RedirectKeepingQuery({ to }: { to: string }) {
  const { search } = useLocation()
  return <Navigate to={{ pathname: to, search }} replace />
}
