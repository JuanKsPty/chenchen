import { lazy, Suspense, useEffect } from 'react'
import {
  matchPath,
  Navigate,
  Route,
  Routes,
  useLocation,
  useSearchParams,
} from 'react-router-dom'

import { Isotipo } from '@/components/brand'
import { Toaster } from '@/components/ui/sonner'
import {
  ACCOUNTS_PATH,
  CONNECT_PATH,
  HOME_PATH,
  MOVEMENTS_PATH,
  RULES_PATH,
  SETTINGS_PATH,
  SPENDING_PATH,
} from '@/lib/nav'
import { useSession } from '@/lib/session'
import { LandingPage } from '@/pages/LandingPage'
import {
  ConnectPage,
  loadSignedInApp,
  preloadConnectPage,
  preloadSignedInApp,
} from '@/pages/lazy'
import { NotFoundPage } from '@/pages/NotFoundPage'
import { SignInPage } from '@/pages/SignInPage'
import { SignUpPage } from '@/pages/SignUpPage'

/**
 * The shell and its pages, in their own chunks (pages/lazy). Fetched together with
 * the page at the address the app opened on, so the first signed-in screen arrives
 * whole.
 */
const SignedInApp = lazy(() => loadSignedInApp(window.location.pathname))

/**
 * Every signed-in path. Signed out, each of them (and every old one below) goes to
 * the sign-in form, which returns there afterwards, query and all: Claude's consent
 * page (/conectar?req=…) is answered after signing in.
 */
const PROTECTED_PATHS = [
  HOME_PATH,
  ACCOUNTS_PATH,
  `${ACCOUNTS_PATH}/:id`,
  MOVEMENTS_PATH,
  SPENDING_PATH,
  RULES_PATH,
  SETTINGS_PATH,
  CONNECT_PATH,
]

/** Addresses from before the redesign, kept working. */
const OLD_PATHS = ['/panel', '/historial', '/reglas', '/seguridad']

const isAppAddress = (pathname: string) =>
  [...PROTECTED_PATHS, ...OLD_PATHS].some((path) => matchPath(path, pathname))

export function App() {
  const { status } = useSession()
  const { pathname } = useLocation()

  const connecting = Boolean(matchPath(CONNECT_PATH, pathname))

  // A visit that opens on a signed-in address most likely has a session to
  // restore: fetch the shell while the refresh cookie is exchanged, not after.
  // Claude's consent page needs only its own chunk.
  useEffect(() => {
    if (status !== 'restoring') return
    if (connecting) preloadConnectPage()
    else if (isAppAddress(pathname)) preloadSignedInApp(pathname)
  }, [status, pathname, connecting])

  // While the refresh cookie is being exchanged the app does not yet know whether it
  // is signed in. Rendering either the sign-in form or Inicio now would flash the
  // wrong screen, so it renders neither.
  if (status === 'restoring') {
    return <RestoringSession />
  }

  if (status === 'signed-out') {
    return (
      <>
        <Routes>
          {/* Three public routes. The portada is a page in its own right, and each
              form has its own URL so either can be linked to or bookmarked. */}
          <Route path="/" element={<LandingPage />} />
          <Route path="/entrar" element={<SignInPage />} />
          <Route path="/registro" element={<SignUpPage />} />
          {/* A signed-in address reached without a session goes to the sign-in form,
              which then returns you to it. */}
          {[...PROTECTED_PATHS, ...OLD_PATHS].map((path) => (
            <Route
              key={path}
              path={path}
              element={path === CONNECT_PATH ? <ConnectSignedOut /> : <RedirectToSignIn />}
            />
          ))}
          <Route path="*" element={<NotFoundPage public />} />
        </Routes>
        <Toaster />
      </>
    )
  }

  // Claude's consent page stands alone: a step in Claude's sign-in, answered and
  // left, with no bars, no assistant and no shell to download.
  if (connecting) {
    return (
      <Suspense fallback={<RestoringSession />}>
        <ConnectPage />
      </Suspense>
    )
  }

  // Until the chunk is in, the screen stays the one the session restore showed: no
  // bars without a page, no flash.
  return (
    <Suspense fallback={<RestoringSession />}>
      <SignedInApp />
    </Suspense>
  )
}

/**
 * Sends a visitor without a session to the sign-in form, remembering where they were
 * headed so a session that expires mid-navigation returns them there instead of to
 * Inicio.
 */
function RedirectToSignIn() {
  const location = useLocation()
  return (
    <Navigate
      to="/entrar"
      replace
      state={{ from: location.pathname + location.search + location.hash }}
    />
  )
}

/**
 * /conectar without a session. A request (?req=) is answered signed in, so it goes
 * through the sign-in form like any signed-in address. A refusal (?error= alone) has
 * nothing to answer and nothing private in it: it is explained right away, rather
 * than after a sign-in that changes nothing.
 */
function ConnectSignedOut() {
  const [params] = useSearchParams()
  if (!params.has('error') || params.has('req')) return <RedirectToSignIn />
  return (
    <Suspense fallback={<RestoringSession />}>
      <ConnectPage />
    </Suspense>
  )
}

function RestoringSession() {
  return (
    <div className="flex min-h-dvh items-center justify-center bg-background px-4">
      <div className="flex flex-col items-center gap-4">
        <Isotipo size={56} title={null} />
        {/* Announced rather than only drawn, so the wait is not silent to a screen
            reader. */}
        <p role="status" className="text-small font-medium text-muted-foreground">
          Abriendo tu chenchén…
        </p>
      </div>
    </div>
  )
}
