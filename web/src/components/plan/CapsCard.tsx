/**
 * <CapsCard plan names failed? onAdd? onEdit? className? />
 *
 *   names   the categories' names by id (categoryNames); undefined while they load,
 *           when the bars wait as skeletons rather than read "Otra categoría"
 *   failed  the names could not be loaded: LoadError in place of the bars, as
 *           Gastos' "Por categoría" does, rather than skeletons that never end
 *   onAdd   "Poner tope", the card's ghost action; left out when the month can no
 *           longer be edited
 *   onEdit  tapping a cap opens it; left out likewise
 *
 * "Topes por categoría", #topes. One PlanBar per cap: what the category's free
 * spending (its children's included) took of it, "Quedan $180.00 de $300.00", or
 * past it, "$42.10 más de lo previsto · tope $300.00" with the excess inked. A cap
 * is a view inside what is free and never comes off it, so the caps never add up to
 * more than "Llevas".
 *
 * What went out with no category counts in what is free but under no cap; the
 * "Sin categoría" to-do (CategoryBar's) closes the card, with the line that says
 * so, and leads to those movements. Bars in as many 280px columns as fit, like
 * Gastos' "Por categoría".
 */
import type { MonthPlan, PlanCapLine } from '@/api/types'
import { CategoryBar, PlanBar, SectionCard } from '@/components/kit'
import { Button } from '@/components/ui/button'
import { LoadError } from '@/components/spending/LoadError'
import { Skeleton } from '@/components/ui/skeleton'
import { monthBounds } from '@/lib/format'
import { movementsHref, PLAN_SECTIONS } from '@/lib/nav'
import { monthWord } from './words'

const GRID =
  'grid grid-cols-[repeat(auto-fit,minmax(min(17.5rem,100%),1fr))] gap-x-10 gap-y-2'

export function CapsCard({
  plan,
  names,
  failed = false,
  onAdd,
  onEdit,
  className,
}: {
  plan: MonthPlan
  names: Map<string, string> | undefined
  failed?: boolean
  onAdd?: () => void
  onEdit?: (cap: PlanCapLine) => void
  className?: string
}) {
  const { from, to } = monthBounds(plan.month)
  const loose = plan.uncategorized
  const showLoose = plan.state !== 'future' && loose.count > 0 && loose.spent.cents > 0

  return (
    <SectionCard
      id={PLAN_SECTIONS.caps}
      title="Topes por categoría"
      className={className}
      action={
        onAdd && (
          <Button variant="ghost" onClick={onAdd}>
            Poner tope
          </Button>
        )
      }
    >
      <div className="flex flex-col gap-4">
        {plan.caps.length === 0 ? (
          <p className="text-body text-pretty text-muted-foreground">
            {onAdd
              ? 'Ponle tope a una categoría y ve cuánto le queda cada mes.'
              : `No tenías topes en ${monthWord(plan.month)}.`}
          </p>
        ) : !names && failed ? (
          <LoadError />
        ) : !names ? (
          <div className={GRID} aria-hidden="true">
            {plan.caps.map((cap) => (
              <div
                key={cap.category_id}
                className="flex min-h-16 flex-col justify-center gap-2"
              >
                <Skeleton className="h-5 w-2/3" />
                <Skeleton className="h-2" />
                <Skeleton className="h-4 w-1/2" />
              </div>
            ))}
          </div>
        ) : (
          <div className={GRID}>
            {plan.caps.map((cap) => (
              <PlanBar
                key={cap.category_id}
                name={names.get(cap.category_id) ?? 'Otra categoría'}
                amount={cap.spent}
                cap={cap.amount}
                onClick={onEdit && (() => onEdit(cap))}
              />
            ))}
          </div>
        )}
        {showLoose && (
          <div className="flex flex-col gap-2">
            <p className="text-small font-medium text-pretty text-muted-foreground">
              Lo que está sin categoría cuenta en lo libre, no en los topes.
            </p>
            <CategoryBar
              variant="uncategorized"
              amount={loose.spent}
              count={loose.count}
              to={movementsHref({ uncategorized: true, from, to })}
            />
          </div>
        )}
      </div>
    </SectionCard>
  )
}
