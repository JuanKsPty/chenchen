import { Link } from 'react-router-dom'

import type { ApiError } from '@/api/client'
import { Wordmark } from '@/components/brand'
import { shell } from '@/lib/layout'
import { cn } from '@/lib/utils'

/**
 * The frame of the signed-out pages: the portada, Entrar and Registro.
 *
 * They use the app's own `shell` container (spec landingHero): 1120px of content
 * with the 16/32 gutter, so the wordmark sits where the signed-in bar's does and
 * does not jump between pages.
 */

/**
 * The 72px bar with the wordmark alone, 28px tall. No buttons: the portada's hero
 * already holds both ways in. On the forms the wordmark links back to the portada.
 */
export function PublicHeader({ linkHome = false }: { linkHome?: boolean }) {
  return (
    // 72px under the notch: index.html sets viewport-fit=cover, as in the shell.
    <header
      className={cn(
        shell,
        'flex h-[calc(4.5rem+env(safe-area-inset-top))] shrink-0 items-center pt-[env(safe-area-inset-top)]',
      )}
    >
      {linkHome ? (
        <Link
          to="/"
          aria-label="chenchén, ir a la portada"
          className="-ml-2 flex h-12 items-center rounded-full px-2"
        >
          <Wordmark size="landing" />
        </Link>
      ) : (
        <Wordmark size="landing" />
      )}
    </header>
  )
}

/**
 * AuthLayout frames the e-mail forms (Entrar and Registro): a 400px column, a card
 * from md and the bare page below it, so a phone gets the whole width for the
 * fields. The heading is the page's h1 in title 32/36; the footer ("¿No tienes
 * cuenta? Crear cuenta") sits under the card.
 */
export function AuthLayout({
  title,
  subtitle,
  children,
  footer,
}: {
  title: string
  subtitle?: React.ReactNode
  children: React.ReactNode
  footer?: React.ReactNode
}) {
  return (
    <div className="flex min-h-dvh flex-col bg-background">
      <PublicHeader linkHome />
      <main className="flex flex-1 flex-col items-center px-4 pt-6 pb-12 md:justify-center md:px-8 md:pt-0 md:pb-24">
        <div className="w-full max-w-100 md:rounded-2xl md:bg-card md:p-8 md:shadow-card">
          <h1 className="font-display text-title font-bold text-balance">{title}</h1>
          {subtitle && (
            <p className="mt-2 text-body text-pretty text-muted-foreground">{subtitle}</p>
          )}
          <div className="mt-8">{children}</div>
        </div>
        {footer && (
          <div className="mt-6 flex w-full max-w-100 flex-wrap items-center justify-center gap-x-1 text-body text-muted-foreground">
            {footer}
          </div>
        )}
      </main>
    </div>
  )
}

/**
 * Under a server fault (5xx) only, the start of the request id: it is what makes
 * the failure findable in the logs. An ordinary refusal shows nothing extra.
 */
export function Reference({ failure }: { failure: ApiError }) {
  if (failure.status < 500 || !failure.requestId) return null
  return (
    <span className="mt-1 block font-normal select-all">
      Referencia {failure.requestId.slice(0, 8)}
    </span>
  )
}
