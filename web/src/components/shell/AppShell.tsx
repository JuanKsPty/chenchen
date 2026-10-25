/**
 * The signed-in chrome, and the providers every signed-in screen can count on.
 *
 * Frozen hooks available under it (F6):
 *   useAssistant()  @/components/assistant/AssistantProvider
 *                   { open, setOpen, toggle, ask(text), send, clear, turns, … }
 *   useImport()     @/components/import/ImportProvider
 *                   { openImport({ accountId? }) }
 *   useTopBar()     @/components/shell/TopBar
 *                   ({ title?, back?, action?, menu? }), the mobile bar
 *   toast()         from 'sonner'; the <Toaster/> is mounted here: top centre below
 *                   lg (clear of the tab bar and bottom sheets), bottom centre from lg
 *
 * Layout. The chrome switches at lg (1024px), not md: at the spec's sizes the
 * desktop bar does not fit a 768px tablet. It is rendered once and revealed by CSS:
 * the desktop bar from lg, the mobile top bar and the tab bar below it. Pages pair
 * with it on the same breakpoint: anything that duplicates the mobile bar (the h1,
 * an overflow button beside the title) shows from lg, and useTopBar covers below.
 * `<main>` exists
 * exactly once at every width, so crossing the breakpoint never remounts a page or
 * loses what somebody was typing. Only the assistant needs JavaScript to choose a
 * surface (a modal sheet cannot simply be hidden on desktop), see AssistantSurface.
 *
 * `<main>`: the shell's 1120px column with its 16 / 32px gutter; 8px under the
 * mobile bar and 40 under the desktop one; at the bottom, room for the tab bar plus
 * 16 on mobile (64 + safe area + 16), 64 on desktop. Pages set their own gaps.
 * With the assistant open from 1280px the column moves left by the panel's 432px.
 *
 * Navigating to a new path scrolls to the top, or to `#id` when the address has one
 * and that element is already on the page (/ajustes#seguridad). A page whose target
 * appears only after its data loads scrolls to it itself. A hash added to the page
 * already open (/gastos → /gastos#transferencias) scrolls to its target too.
 *
 * A page that throws while rendering shows "Algo se rompió de nuestro lado." with
 * "Recargar" inside <main>, and the bars stay (ErrorBoundary). A page whose chunk is
 * still loading (pages/lazy) shows PageFallback there, the bars staying too.
 */
import { Suspense, useEffect, type ReactNode } from 'react'
import { useLocation } from 'react-router-dom'

import { AssistantProvider, useAssistant } from '@/components/assistant/AssistantProvider'
import { ErrorBoundary } from '@/components/ErrorBoundary'
import { ImportProvider } from '@/components/import/ImportProvider'
import { Toaster } from '@/components/ui/sonner'
import { TooltipProvider } from '@/components/ui/tooltip'
import { shell } from '@/lib/layout'
import { cn } from '@/lib/utils'
import { AssistantSurface } from './AssistantSurface'
import { BottomTabBar } from './BottomTabBar'
import { DesktopTopBar } from './DesktopTopBar'
import { DESKTOP_QUERY, PUSH_QUERY, useMediaQuery, useScrolled } from './media'
import { MobileAppBar } from './MobileAppBar'
import { PageFallback } from './PageFallback'
import { TopBarProvider } from './TopBar'

export function AppShell({ children }: { children: ReactNode }) {
  return (
    // Required by every shadcn component that can carry a tooltip; only signed-in
    // screens have any, so the signed-out pages' chunk goes without it. A short
    // delay, so a pointer passing through does not trigger one that reads as noise.
    <TooltipProvider delayDuration={300}>
      <AssistantProvider>
        <ImportProvider>
          <TopBarProvider>
            <ShellLayout>{children}</ShellLayout>
          </TopBarProvider>
        </ImportProvider>
      </AssistantProvider>
    </TooltipProvider>
  )
}

function ShellLayout({ children }: { children: ReactNode }) {
  const { open } = useAssistant()
  const desktop = useMediaQuery(DESKTOP_QUERY)
  const pushes = useMediaQuery(PUSH_QUERY)
  const scrolled = useScrolled()
  // From lg to xl the open panel floats over a scrim that keeps the pointer off the
  // page; the page is inert too, so Tab and a screen reader stay in the panel. (The
  // mobile bars are hidden there, and the sheet below lg is a modal of its own.)
  const covered = open && desktop && !pushes
  const location = useLocation()

  useScrollOnNavigate(location.pathname, location.hash, location.key)

  return (
    <div className="flex min-h-dvh flex-col bg-background">
      {/* The first thing a keyboard reaches, and the way past the bar. */}
      <a
        href="#contenido"
        inert={covered}
        className={cn(
          'sr-only focus:not-sr-only focus:fixed focus:top-3 focus:left-4 focus:z-60',
          'focus:inline-flex focus:h-11 focus:items-center focus:rounded-full focus:bg-inverse focus:px-5',
          'focus:text-body focus:font-semibold focus:text-inverse-foreground',
        )}
      >
        Saltar al contenido
      </a>

      <DesktopTopBar scrolled={scrolled} inert={covered} />
      <MobileAppBar scrolled={scrolled} />

      <div
        inert={covered}
        className={cn(
          'flex flex-1 flex-col transition-[padding] duration-280 ease-sheet',
          open && 'xl:pr-108',
        )}
      >
        <main
          id="contenido"
          tabIndex={-1}
          // A new page is a new component tree: state from the previous one (a
          // half-typed filter, an open sheet) never leaks into it.
          key={location.pathname}
          className={cn(
            shell,
            'flex-1 pt-2 pb-[calc(5rem+env(safe-area-inset-bottom))] outline-none lg:pt-10 lg:pb-16',
          )}
        >
          {/* A page that fails to render (or whose chunk fails to load) says so
              here and keeps the chrome; keyed with <main>, so another section
              starts afresh. A page still loading shows its fallback, bars and all. */}
          <ErrorBoundary variant="page">
            <Suspense fallback={<PageFallback />}>{children}</Suspense>
          </ErrorBoundary>
        </main>
      </div>

      {/* Covered by the full-screen sheet anyway; left out so it cannot be reached
          behind it. */}
      {!(open && !desktop) && <BottomTabBar />}

      <AssistantSurface desktop={desktop} />
      <Toaster />
    </div>
  )
}

/**
 * Scrolls on every new path, and on every navigation to a hash, including a hash
 * added to the page already open and a second tap on the same "#transferencias"
 * link after scrolling away (a new history entry, so a new `key`). A change of the
 * query string alone (Movimientos' filters) leaves the scroll where it is.
 */
function useScrollOnNavigate(pathname: string, hash: string, key: string) {
  const hashVisit = hash ? key : null
  useEffect(() => {
    const target = hash ? document.getElementById(decodeURIComponent(hash.slice(1))) : null
    if (!target) {
      window.scrollTo(0, 0)
      return
    }
    // Clear of whichever sticky bar is showing (the hidden one measures 0), plus 16.
    const bars = document.querySelectorAll<HTMLElement>('[data-shell-bar]')
    const covered = Math.max(0, ...Array.from(bars, (bar) => bar.offsetHeight))
    const top = target.getBoundingClientRect().top + window.scrollY - covered - 16
    window.scrollTo(0, Math.max(0, top))
  }, [pathname, hash, hashVisit])
}
