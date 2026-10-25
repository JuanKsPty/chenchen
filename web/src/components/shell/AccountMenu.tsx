import { LogOutIcon, SettingsIcon } from 'lucide-react'
import { Link } from 'react-router-dom'

import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { SETTINGS_PATH } from '@/lib/nav'
import { useSession } from '@/lib/session'
import { cn } from '@/lib/utils'

/**
 * The avatar and its menu: Ajustes and Salir, nothing else (spec menu).
 *
 * `bar`: the desktop bar's 40px surface2 circle with the initial at 16/24 600.
 * `mobile`: a 44px target around a 32px circle, initial at 14/20 600.
 */
export function AccountMenu({ size }: { size: 'bar' | 'mobile' }) {
  const { user, signOut } = useSession()
  const firstName = user?.full_name.trim().split(/\s+/)[0] ?? ''
  const initial = (firstName.charAt(0) || '?').toUpperCase()

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        {size === 'bar' ? (
          <Button
            variant="secondary"
            size="icon-bar"
            aria-label={firstName ? `Menú de ${firstName}` : 'Menú de tu cuenta'}
          >
            {initial}
          </Button>
        ) : (
          <Button
            variant="plain"
            size="icon"
            aria-label={firstName ? `Menú de ${firstName}` : 'Menú de tu cuenta'}
            className="hover:bg-transparent aria-expanded:bg-transparent"
          >
            <span
              className={cn(
                'flex size-8 items-center justify-center rounded-full bg-muted text-small font-semibold',
                'transition-colors duration-180 ease-out group-hover/button:bg-line group-aria-expanded/button:bg-line',
              )}
            >
              {initial}
            </span>
          </Button>
        )}
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-auto">
        <DropdownMenuItem asChild>
          <Link to={SETTINGS_PATH}>
            <SettingsIcon aria-hidden="true" />
            Ajustes
          </Link>
        </DropdownMenuItem>
        <DropdownMenuItem onSelect={() => void signOut()}>
          <LogOutIcon aria-hidden="true" />
          Salir
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
