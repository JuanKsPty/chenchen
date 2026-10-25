/**
 * <ErrorBoundary variant="page" | "app">{children}</ErrorBoundary>
 *
 * What shows when rendering throws, instead of a blank screen (spec voice: short,
 * no blame, a verb on the button):
 *
 *   "Algo se rompió de nuestro lado."
 *   "Tus datos están bien. Recarga la página para seguir."   [Recargar]
 *
 *   page  inside the AppShell's <main>: the bars and the tab bar stay, so the other
 *         sections are one tap away; "Recargar" is secondary next to the bar's
 *         primary. The shell keys <main> by path, so moving to
 *         another section starts a fresh boundary.
 *   app   around the whole app (main.tsx), for a failure in the shell itself: a
 *         full page with the isotipo, like the session-restore screen. It uses no
 *         router, session or query context, since any of them may be what broke.
 *
 * "Recargar" reloads the page: a render error is rarely fixed by rendering the
 * same tree again, and a reload also picks up a newer build.
 */
import { Component, type ErrorInfo, type ReactNode } from 'react'

import { Isotipo } from '@/components/brand'
import { EmptyState } from '@/components/kit'
import { Button } from '@/components/ui/button'

const TITLE = 'Algo se rompió de nuestro lado.'
const DESCRIPTION = 'Tus datos están bien. Recarga la página para seguir.'

function reload() {
  window.location.reload()
}

interface Props {
  variant: 'page' | 'app'
  children: ReactNode
}

export class ErrorBoundary extends Component<Props, { failed: boolean }> {
  state = { failed: false }

  static getDerivedStateFromError() {
    return { failed: true }
  }

  componentDidCatch(error: unknown, info: ErrorInfo) {
    // Left in the console for whoever opens the developer tools; nothing is sent.
    console.error(error, info.componentStack)
  }

  render() {
    if (!this.state.failed) return this.props.children
    return this.props.variant === 'app' ? <AppFailure /> : <PageFailure />
  }
}

function PageFailure() {
  return (
    // "Recargar" is secondary here: the bar around it may already show its primary
    // ("Importar estado"), and a view gets one.
    <div role="alert">
      <EmptyState
        headingLevel={1}
        illustration={null}
        title={TITLE}
        description={DESCRIPTION}
        className="md:py-16"
      >
        <Button
          variant="secondary"
          size="lg"
          className="mt-2 w-full md:h-11 md:w-auto"
          onClick={reload}
        >
          Recargar
        </Button>
      </EmptyState>
    </div>
  )
}

function AppFailure() {
  return (
    <main className="flex min-h-dvh items-center justify-center bg-background px-4">
      <div role="alert" className="flex max-w-90 flex-col items-center gap-4 text-center">
        <Isotipo size={56} title={null} />
        <h1 className="font-display text-heading font-bold text-balance">{TITLE}</h1>
        <p className="text-body text-pretty text-muted-foreground">{DESCRIPTION}</p>
        <Button size="lg" className="mt-2 w-full md:h-11 md:w-auto" onClick={reload}>
          Recargar
        </Button>
      </div>
    </main>
  )
}
