import { Toaster as Sonner, type ToasterProps } from 'sonner'
import { CircleCheckIcon, InfoIcon, CircleAlertIcon, Loader2Icon } from 'lucide-react'

import { DESKTOP_QUERY, useMediaQuery } from '@/components/shell/media'
import { useTheme } from '@/lib/theme'

/**
 * Toaster, per the spec's toast: inverse colours, 16px radius, raised shadow,
 * small 14/20 500 text, at least 52px tall, padding 4 4 4 16, 5 seconds (sonner
 * pauses on hover and focus). An action ("Deshacer", "Ver regla") is a 44px
 * transparent pill in inverse text, 600 and underlined.
 *
 * Where it sits follows the chrome's breakpoint (lg, 1024px):
 *   below lg  top centre, 8px under the safe area, over the top bar. At the bottom
 *             it would cover the tab bar, a bottom sheet and the sheet's primary
 *             button ("Guardar", "Importar") for its 5 seconds. Swiped up to go.
 *   from lg   bottom centre, 24px up.
 * It renders above every scrim and dialog (sonner's own z-index), and its live
 * region stays audible while a modal is open (aria-hidden keeps [aria-live]).
 *
 * Mounted once in the shell and once on the signed-out pages. Call
 * `toast('Guardado', { action: { label: 'Deshacer', onClick } })` from `sonner`.
 */
const TOP_OFFSET = 'calc(env(safe-area-inset-top) + 0.5rem)'

const Toaster = ({ ...props }: ToasterProps) => {
  const { resolvedTheme } = useTheme()
  const desktop = useMediaQuery(DESKTOP_QUERY)

  return (
    <Sonner
      theme={resolvedTheme}
      position={desktop ? 'bottom-center' : 'top-center'}
      duration={5000}
      gap={8}
      className="toaster group"
      // The region's name, in the app's language (sonner says "Notifications"; it
      // adds the shortcut, alt+T, that moves focus to the toasts).
      containerAriaLabel="Notificaciones"
      // sonner uses mobileOffset up to 600px wide and offset above that; both
      // keep 16px at the sides below lg.
      mobileOffset={{ top: TOP_OFFSET }}
      offset={desktop ? { bottom: '1.5rem' } : { top: TOP_OFFSET, left: '1rem', right: '1rem' }}
      icons={{
        success: <CircleCheckIcon className="size-5" />,
        info: <InfoIcon className="size-5" />,
        warning: <CircleAlertIcon className="size-5" />,
        error: <CircleAlertIcon className="size-5" />,
        loading: <Loader2Icon className="size-5 animate-spin" />,
      }}
      style={
        {
          '--normal-bg': 'var(--inverse)',
          '--normal-text': 'var(--inverse-foreground)',
          '--normal-border': 'var(--inverse)',
          '--border-radius': 'var(--radius)',
        } as React.CSSProperties
      }
      toastOptions={{
        closeButtonAriaLabel: 'Cerrar aviso',
        classNames: {
          // Sonner's own stylesheet is unlayered, so these need `!` to win.
          toast:
            // pointer-events-auto: a modal dialog turns pointer events off on <body>,
            // and the toast above its scrim must still take a tap on "Deshacer".
            'cn-toast pointer-events-auto! min-h-13! gap-2! py-1! pr-1! pl-4! font-sans! text-small! font-medium! shadow-raised! border-0!',
          title: 'text-small! font-medium!',
          description: 'text-small! text-inverse-foreground!',
          actionButton:
            'ml-auto! h-11! shrink-0! rounded-full! bg-transparent! px-3! text-small! font-semibold! text-inverse-foreground! underline! underline-offset-2! hover:bg-inverse-foreground/12!',
          cancelButton:
            'h-11! shrink-0! rounded-full! bg-transparent! px-3! text-small! font-semibold! text-inverse-foreground! hover:bg-inverse-foreground/12!',
          closeButton: 'border-0! bg-inverse! text-inverse-foreground!',
        },
      }}
      {...props}
    />
  )
}

export { Toaster }
