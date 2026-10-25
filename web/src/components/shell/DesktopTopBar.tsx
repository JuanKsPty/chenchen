import { FileUpIcon } from 'lucide-react'
import { Link, NavLink } from 'react-router-dom'

import { useAssistant } from '@/components/assistant/AssistantProvider'
import { AssistantIcon, Wordmark } from '@/components/brand'
import { useImport } from '@/components/import/ImportProvider'
import { Button } from '@/components/ui/button'
import { shell } from '@/lib/layout'
import { HOME_PATH, NAV } from '@/lib/nav'
import { useAccounts } from '@/lib/queries'
import { cn } from '@/lib/utils'
import { AccountMenu } from './AccountMenu'

/**
 * The desktop bar (spec desktopNav), from lg up: 64px, the page's background, no
 * border, and the bar shadow once the page has scrolled under it.
 *
 * Left, the wordmark home; 40px on, the four sections as pill tabs in a surface2
 * track. The active tab is a white pill with the card shadow, never pink: the
 * veranera belongs to the one primary button. Right: the Asistente toggle (inverse
 * while the panel is open), "Importar estado" and the avatar menu.
 *
 * Why lg and not md: at the spec's sizes the bar needs about 830px, more than a
 * 768px tablet has, so below 1024px the phone's chrome takes over (MobileAppBar and
 * BottomTabBar). That also retires the spec's icon-only Asistente "below 1024px".
 * Even at 1024 it is close: the wordmark keeps its 40px to the tabs, but the
 * actions only keep 16px to them.
 *
 * One primary per view: with no accounts yet, every page's empty state carries
 * its own import button, so the bar leaves "Importar estado" out. Only once the
 * accounts have loaded and are none: while they load the button stays, so it never
 * flickers away and back for somebody who has accounts.
 */
export function DesktopTopBar({ scrolled, inert }: { scrolled: boolean; inert?: boolean }) {
  const { open, toggle } = useAssistant()
  const { openImport } = useImport()
  const accounts = useAccounts()
  const noAccounts = accounts.data?.accounts.length === 0

  return (
    <header
      data-shell-bar
      inert={inert}
      className={cn(
        'sticky top-0 z-30 hidden h-16 bg-background transition-shadow duration-180 ease-out lg:block',
        scrolled && 'shadow-bar',
      )}
    >
      <div className={cn(shell, 'flex h-full items-center gap-4')}>
        {/* The way home, not a section: a plain Link, so it never claims
            aria-current next to the Inicio tab. */}
        <Link
          to={HOME_PATH}
          aria-label="chenchén, Inicio"
          className="mr-6 flex h-12 shrink-0 items-center rounded-full"
        >
          <Wordmark size="bar" />
        </Link>

        <nav
          aria-label="Secciones"
          className="flex h-12 shrink-0 items-center gap-1 rounded-full bg-muted p-1"
        >
          {NAV.map((item) => (
            <NavLink
              key={item.to}
              to={item.to}
              className={({ isActive }) =>
                cn(
                  'inline-flex h-10 items-center rounded-full px-4.5 text-body font-semibold whitespace-nowrap transition-colors duration-180 ease-out',
                  isActive
                    ? 'bg-card text-foreground shadow-card'
                    : 'text-muted-foreground hover:text-foreground',
                )
              }
            >
              {item.label}
            </NavLink>
          ))}
        </nav>

        <div className="ml-auto flex shrink-0 items-center gap-2">
          <Button
            variant="tonal"
            size="bar"
            aria-pressed={open}
            onClick={toggle}
            className="pr-4 pl-3 has-[>svg:first-child]:pl-3"
          >
            <AssistantIcon variant="desktop" />
            Asistente
          </Button>

          {!noAccounts && (
            <Button size="bar" onClick={() => openImport()}>
              <FileUpIcon aria-hidden="true" />
              Importar estado
            </Button>
          )}

          <AccountMenu size="bar" />
        </div>
      </div>
    </header>
  )
}
