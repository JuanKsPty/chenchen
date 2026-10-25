/**
 * <PickerSheet open onOpenChange title onCloseAutoFocus? children />
 *
 * The phone's surface for a filter's choices (below lg): a bottom sheet with the
 * 36×4 grab handle, the title in heading 22, and its body scrolling under it. A drag
 * down on the handle or the title closes it, like the scrim, Escape and "Cerrar".
 *
 * <PickerRow onClick icon? detail? pending? children />  one 52px choice in it:
 * a 20px muted icon, the label in body 500, an optional muted detail at the right.
 *
 * <PickerGroupTitle>  a group's heading inside the sheet ("Gastos", "Entradas").
 */
import type { LucideIcon } from 'lucide-react'
import type { ComponentProps, ReactNode } from 'react'

import { Sheet, SheetContent, SheetHeader, SheetTitle } from '@/components/ui/sheet'
import { cn } from '@/lib/utils'
import { useSwipeDown } from './useSwipeDown'

export function PickerSheet({
  open,
  onOpenChange,
  title,
  onCloseAutoFocus,
  children,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  title: string
  onCloseAutoFocus?: (event: Event) => void
  children: ReactNode
}) {
  const swipe = useSwipeDown(() => onOpenChange(false))

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        side="bottom"
        className="gap-0"
        aria-describedby={undefined}
        onCloseAutoFocus={onCloseAutoFocus}
      >
        <SheetHeader className="relative touch-none pb-1" {...swipe}>
          <SheetHandle />
          <SheetTitle>{title}</SheetTitle>
        </SheetHeader>
        <div className="min-h-0 flex-1 overflow-y-auto px-2 pb-4">{children}</div>
      </SheetContent>
    </Sheet>
  )
}

/** The bottom sheet's grab handle: 36×4, muted, 8px from the top. Decorative. */
export function SheetHandle() {
  return (
    <span
      aria-hidden="true"
      className="absolute top-2 left-1/2 h-1 w-9 -translate-x-1/2 rounded-full bg-muted-foreground"
    />
  )
}

export function PickerGroupTitle({ children }: { children: ReactNode }) {
  return (
    <h3 className="px-3 pt-3 pb-1 text-small font-semibold text-muted-foreground">
      {children}
    </h3>
  )
}

export function PickerRow({
  icon: Icon,
  pending = false,
  detail,
  className,
  children,
  ...props
}: ComponentProps<'button'> & {
  icon?: LucideIcon
  /** "Sin categoría": the icon in primary-text, a to-do rather than a category. */
  pending?: boolean
  detail?: ReactNode
}) {
  return (
    <button
      type="button"
      {...props}
      className={cn(
        'flex min-h-13 w-full items-center gap-3 rounded-full px-3 text-left text-body font-medium text-foreground transition-colors duration-180 ease-out hover:bg-muted active:bg-muted',
        className,
      )}
    >
      {Icon && (
        <Icon
          aria-hidden="true"
          className={cn(
            'size-5 shrink-0',
            pending ? 'text-primary-text' : 'text-muted-foreground',
          )}
        />
      )}
      <span className="min-w-0 flex-1 truncate">{children}</span>
      {detail && (
        <span className="shrink-0 text-small font-medium text-muted-foreground">
          {detail}
        </span>
      )}
    </button>
  )
}
