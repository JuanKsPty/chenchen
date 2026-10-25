/**
 * <LoadError className? />
 *
 * The line a Gastos card shows when its figures did not load, in the voice's
 * errorGeneric. Small, muted, never orange: nothing is wrong with the money.
 */
import { cn } from '@/lib/utils'

export function LoadError({ className }: { className?: string }) {
  return (
    <p
      role="status"
      className={cn('text-small font-medium text-pretty text-muted-foreground', className)}
    >
      Algo falló de nuestro lado. Tus datos están bien; intenta otra vez en un momento.
    </p>
  )
}
