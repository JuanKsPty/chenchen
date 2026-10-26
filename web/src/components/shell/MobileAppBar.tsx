import { ChevronLeftIcon, EllipsisIcon, FileUpIcon } from 'lucide-react'
import type { ReactNode } from 'react'
import { Link, matchPath, useLocation } from 'react-router-dom'

import { useAssistant } from '@/components/assistant/AssistantProvider'
import { AssistantIcon, Wordmark } from '@/components/brand'
import { useImport } from '@/components/import/ImportProvider'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import {
  ACCOUNTS_PATH,
  HOME_PATH,
  MOVEMENTS_PATH,
  RULES_PATH,
  SETTINGS_PATH,
  SPENDING_PATH,
} from '@/lib/nav'
import { useSession } from '@/lib/session'
import { cn } from '@/lib/utils'
import { AccountMenu } from './AccountMenu'
import { useTopBarConfig } from './TopBar'

/**
 * The phone's top bar (spec mobileTopBar), below lg: 56px plus the safe area, the
 * page's background, no border, the bar shadow once the page has scrolled.
 *
 * Left: on Inicio the greeting, "Buenas, Juan" over "Así va tu chenchén."; on a root
 * section its name; on a screen you went into, a 44px chevron and its title. Never a
 * chevron on a root section. Right, 4px apart: the page's compact action (Cuentas:
 * "Importar"), the tonal Asistente button, and the avatar, which becomes "Más
 * opciones" when the page has an overflow menu.
 *
 * Pages adjust it with useTopBar(); the defaults per route live here.
 */
export function MobileAppBar({ scrolled }: { scrolled: boolean }) {
  const { pathname } = useLocation()
  const page = useTopBarConfig()
  const route = routeDefaults(pathname)
  const { setOpen } = useAssistant()
  const { openImport } = useImport()

  const title = page?.title ?? route.title
  const back = page?.back ?? route.back
  const menu = page?.menu
  const action =
    page?.action !== undefined
      ? page.action
      : route.importAction && (
          <Button className="pr-4 pl-3" onClick={() => openImport()}>
            <FileUpIcon aria-hidden="true" />
            Importar
          </Button>
        )

  return (
    <header
      data-shell-bar
      className={cn(
        'sticky top-0 z-30 bg-background pt-[env(safe-area-inset-top)] transition-shadow duration-180 ease-out lg:hidden',
        scrolled && 'shadow-bar',
      )}
    >
      <div className={cn('flex h-14 items-center gap-1 pr-1', back ? 'pl-1' : 'pl-4')}>
        <div className="flex min-w-0 flex-1 items-center gap-1">
          {back && (
            <Button asChild variant="plain" size="icon">
              <Link to={back} aria-label="Volver">
                <ChevronLeftIcon className="size-6" aria-hidden="true" />
              </Link>
            </Button>
          )}
          {route.greeting && !page?.title ? (
            <Greeting />
          ) : title ? (
            <p className="truncate font-display text-heading font-bold">{title}</p>
          ) : (
            !back && (
              <Link
                to={HOME_PATH}
                aria-label="chenchén, Inicio"
                className="flex h-11 items-center rounded-full"
              >
                <Wordmark size="mobile" />
              </Link>
            )
          )}
        </div>

        <div className="flex shrink-0 items-center gap-1">
          {action}
          <Button
            variant="tonal"
            size="icon"
            aria-label="Abrir asistente"
            onClick={() => setOpen(true)}
          >
            <AssistantIcon variant="mobile" className="size-6" />
          </Button>
          {menu ? <OverflowMenu>{menu}</OverflowMenu> : <AccountMenu size="mobile" />}
        </div>
      </div>
    </header>
  )
}

function Greeting() {
  const { user } = useSession()
  const firstName = user?.full_name.trim().split(/\s+/)[0]

  return (
    <div className="flex min-w-0 flex-col">
      <p className="truncate font-display text-heading font-bold">
        {firstName ? `Buenas, ${firstName}` : 'Buenas'}
      </p>
      <p className="truncate text-small font-medium text-muted-foreground">
        Así va tu chenchén.
      </p>
    </div>
  )
}

function OverflowMenu({ children }: { children: ReactNode }) {
  return (
    // Non-modal: an item that opens a dialog would otherwise fight the menu over
    // focus and leave the page unclickable.
    <DropdownMenu modal={false}>
      <DropdownMenuTrigger asChild>
        <Button variant="plain" size="icon" aria-label="Más opciones">
          <EllipsisIcon className="size-6" aria-hidden="true" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-auto">
        {children}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

interface RouteDefaults {
  title?: string
  back?: string
  greeting?: boolean
  importAction?: boolean
}

/**
 * The bar for each route before the page says anything. A table, because the routes
 * are few and a page should not have to know it is drawn inside a phone's chrome
 * unless it is a screen you went into.
 */
function routeDefaults(pathname: string): RouteDefaults {
  if (pathname === HOME_PATH) return { greeting: true }
  if (pathname === ACCOUNTS_PATH) return { title: 'Cuentas', importAction: true }
  if (matchPath(`${ACCOUNTS_PATH}/:id`, pathname)) return { back: ACCOUNTS_PATH }
  if (pathname === MOVEMENTS_PATH) return { title: 'Movimientos' }
  if (pathname === SPENDING_PATH) return { title: 'Gastos' }
  if (pathname === RULES_PATH) return { title: 'Reglas', back: SPENDING_PATH }
  if (pathname === SETTINGS_PATH) return { title: 'Ajustes' }
  return {}
}
