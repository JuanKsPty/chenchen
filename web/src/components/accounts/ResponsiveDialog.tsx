/**
 * <ResponsiveDialogContent title? description? footer? className? children />
 *
 *   title        the dialog's name (Gabarito 700 22/28), wired to aria-labelledby.
 *                Leave it out when the body draws its own headline, and put a
 *                <DialogTitle> there instead (the import result does).
 *   description  under the title; wired to aria-describedby
 *   footer       the buttons. Below lg they stack full width, the last one on top
 *                (put the primary last); from lg they sit in a row at the right.
 *   children     the body, which scrolls between the header and the footer
 *
 * Goes inside a Radix <Dialog> root from '@/components/ui/dialog' (a Sheet is the
 * same primitive, so <DialogTrigger> works either way). Below lg, where the app
 * has its phone chrome, it is the spec's bottom sheet: rounded top, a 36×4 handle,
 * header padded 20 16 8, at most 85dvh, the footer clear of the safe area. From lg
 * it is a centred dialog. Both show the 44px "Cerrar" button.
 */
import type { ReactNode } from 'react'

import { DESKTOP_QUERY, useMediaQuery } from '@/components/shell/media'
import { DialogContent, DialogDescription, DialogTitle } from '@/components/ui/dialog'
import { SheetContent } from '@/components/ui/sheet'
import { cn } from '@/lib/utils'

export function ResponsiveDialogContent({
  title,
  description,
  footer,
  className,
  children,
}: {
  title?: ReactNode
  description?: ReactNode
  footer?: ReactNode
  className?: string
  children?: ReactNode
}) {
  const desktop = useMediaQuery(DESKTOP_QUERY)
  // Radix asks for an explicit undefined when a dialog has no description.
  const noDescription = description ? {} : { 'aria-describedby': undefined }

  const header = title !== undefined && (
    <div
      className={cn(
        'flex shrink-0 flex-col gap-1',
        desktop ? 'px-6 pt-6 pb-2' : 'px-4 pt-5 pb-2',
      )}
    >
      <DialogTitle tabIndex={-1} className="pr-12 font-display outline-none">
        {title}
      </DialogTitle>
      {description && (
        <DialogDescription
          className={cn('text-pretty', desktop ? 'text-body' : 'text-small font-medium')}
        >
          {description}
        </DialogDescription>
      )}
    </div>
  )
  const body = (
    <div
      className={cn(
        'flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto',
        desktop ? 'px-6 py-4' : 'px-4 py-3',
        // Clear of the close button when there is no header above.
        title === undefined && 'pt-8',
      )}
    >
      {children}
    </div>
  )
  const actions = footer && (
    <div
      className={cn(
        'flex shrink-0 gap-2',
        desktop ? 'flex-row justify-end px-6 pt-2 pb-6' : 'flex-col-reverse px-4 pt-3 pb-4',
      )}
    >
      {footer}
    </div>
  )

  if (desktop) {
    return (
      <DialogContent
        {...noDescription}
        className={cn(
          'flex max-h-[min(46rem,calc(100dvh-4rem))] flex-col gap-0 overflow-hidden p-0 sm:max-w-lg',
          className,
        )}
      >
        {header}
        {body}
        {actions}
      </DialogContent>
    )
  }

  return (
    <SheetContent side="bottom" className={cn('gap-0', className)} {...noDescription}>
      <span
        aria-hidden="true"
        className="pointer-events-none absolute top-2 left-1/2 h-1 w-9 -translate-x-1/2 rounded-full bg-muted-foreground"
      />
      {header}
      {body}
      {actions}
    </SheetContent>
  )
}
