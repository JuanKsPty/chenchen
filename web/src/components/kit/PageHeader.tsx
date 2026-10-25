/**
 * <PageHeader title subtitle? leading? actions? className? />
 *
 *   title     the page's <h1>, Gabarito 700 32/36 from lg. Below lg the mobile top
 *             bar already shows the title, so the h1 stays for screen readers only
 *             (sr-only below lg, the shell's chrome breakpoint).
 *   subtitle  small 14/20 500 muted under the title ("Gastos del mes · ···0932").
 *             From lg only, like the title; show it in the page body below.
 *   leading   ReactNode before the title, e.g. <Tile institution="bac" size="lg" />.
 *             From lg only.
 *   actions   ReactNode at the right ("Exportar CSV", the month picker, an overflow
 *             menu). Shown at every size; one that the mobile bar already covers
 *             (the overflow menu via useTopBar) goes `hidden lg:flex`.
 *
 * The h1 is top-aligned, so it starts at the same place on every page; the actions
 * are centred on its line. The title block and the actions wrap onto two lines when
 * they do not fit.
 */
import type { ReactNode } from 'react'

import { cn } from '@/lib/utils'

export function PageHeader({
  title,
  subtitle,
  leading,
  actions,
  className,
}: {
  title: ReactNode
  subtitle?: ReactNode
  leading?: ReactNode
  actions?: ReactNode
  className?: string
}) {
  return (
    <header
      className={cn(
        // Top-aligned: the h1 sits at the top of the page column on every page,
        // whatever stands beside it (40px under the bar from lg).
        'flex flex-wrap items-start justify-between gap-4',
        // Nothing visible on mobile without actions: collapse so the page's own
        // gap does not open an empty band under the top bar.
        !actions && 'max-lg:sr-only',
        className,
      )}
    >
      <div className="flex min-w-0 items-center gap-4 max-lg:sr-only">
        {leading}
        <div className="flex min-w-0 flex-col gap-0.5">
          <h1 className="font-display text-title font-bold text-balance">{title}</h1>
          {subtitle && (
            <p className="text-small font-medium text-muted-foreground">{subtitle}</p>
          )}
        </div>
      </div>
      {actions && (
        // As tall as the h1's line from lg, so a 40 or 44px control is centred on
        // the title instead of pushing it down, and the content below starts the
        // same distance under every title.
        <div className="flex flex-wrap items-center gap-2 lg:h-9">{actions}</div>
      )}
    </header>
  )
}
