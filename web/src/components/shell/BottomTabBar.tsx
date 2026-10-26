import { NavLink } from 'react-router-dom'

import { NAV } from '@/lib/nav'
import { cn } from '@/lib/utils'

/**
 * The phone's tab bar (spec mobileTabBar), below lg: four equal cells, no floating
 * button. The surface colour with the bar shadow, 64px plus the safe area.
 *
 * Each cell is the whole target: a 56×32 pill holding the 24px icon, and the label
 * in micro 600 under it. Active: primary-soft pill, primary-text icon, label in the
 * text colour (told apart from the muted ones by lightness, not only hue).
 *
 * The shell leaves it out while the assistant sheet is open.
 */
export function BottomTabBar() {
  return (
    <nav
      aria-label="Secciones"
      className={cn(
        'fixed inset-x-0 bottom-0 z-30 lg:hidden',
        'bg-card pb-[env(safe-area-inset-bottom)] shadow-bar',
      )}
    >
      <ul className="grid h-16 grid-cols-4">
        {NAV.map((item) => (
          <li key={item.to} className="flex min-w-0">
            <NavLink
              to={item.to}
              className={({ isActive }) =>
                cn(
                  'flex min-w-0 flex-1 flex-col items-center gap-1 pt-2 text-micro font-semibold transition-colors duration-180 ease-out',
                  isActive ? 'text-foreground' : 'text-muted-foreground',
                )
              }
            >
              {({ isActive }) => (
                <>
                  <span
                    className={cn(
                      'flex h-8 w-14 items-center justify-center rounded-full transition-colors duration-180 ease-out',
                      isActive && 'bg-primary-soft text-primary-text',
                    )}
                  >
                    <item.icon className="size-6" aria-hidden="true" />
                  </span>
                  <span className="max-w-full truncate">{item.label}</span>
                </>
              )}
            </NavLink>
          </li>
        ))}
      </ul>
    </nav>
  )
}
