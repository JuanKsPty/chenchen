/**
 * <TopCategories className? />
 *
 * "En qué se te va": this month's four biggest spending categories (spec
 * categoryBar), each a bar scaled against the biggest one and a link to
 * Movimientos filtered by that category and month. "Ver todo" goes to Gastos,
 * where every category and "Sin categoría" live. Movements with no category are
 * left out here: they are a to-do, not a category.
 */
import { Link } from 'react-router-dom'

import { CategoryBar, SectionCard } from '@/components/kit'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { categoryNames } from '@/lib/categories'
import { currentMonth, monthBounds } from '@/lib/format'
import { SPENDING_PATH, movementsHref } from '@/lib/nav'
import { useCategories, useCategoryTotals } from '@/lib/queries'

const SHOWN = 4

export function TopCategories({ className }: { className?: string }) {
  const { from, to } = monthBounds(currentMonth())
  const totals = useCategoryTotals({ from, to, kind: 'spend' })
  const categories = useCategories()

  const names = categoryNames(categories.data?.categories)
  const top = (totals.data?.totals ?? [])
    .filter((row) => row.category_id !== null && row.total.cents > 0)
    .sort((a, b) => b.total.cents - a.total.cents)
    .slice(0, SHOWN)
  const max = top[0]?.total.cents ?? 0

  let body
  if (totals.isLoading || categories.isLoading) {
    body = <CategorySkeleton />
  } else if (totals.isError) {
    body = (
      <p className="text-body text-pretty text-muted-foreground">
        No pudimos cargar tus gastos. Intenta otra vez en un momento.
      </p>
    )
  } else if (top.length === 0) {
    body = <p className="text-body text-muted-foreground">Aún no hay gastos este mes.</p>
  } else {
    body = (
      <ul className="flex flex-col gap-2 lg:gap-3">
        {top.map((row) => {
          const id = row.category_id as string
          return (
            <li key={id}>
              <CategoryBar
                name={names.get(id) ?? 'Otra categoría'}
                amount={row.total}
                max={max}
                to={movementsHref({ category: id, from, to })}
              />
            </li>
          )
        })}
      </ul>
    )
  }

  return (
    <SectionCard
      title="En qué se te va"
      action={
        <Button asChild variant="ghost">
          <Link to={SPENDING_PATH} aria-label="Ver todo en Gastos">
            Ver todo
          </Link>
        </Button>
      }
      className={className}
    >
      {body}
    </SectionCard>
  )
}

function CategorySkeleton() {
  return (
    <ul aria-hidden="true" className="flex flex-col gap-2 lg:gap-3">
      {[0.5, 0.35, 0.45, 0.3].map((width, index) => (
        <li key={index} className="flex min-h-13 flex-col justify-center gap-3 py-1">
          <span className="flex justify-between gap-3">
            <Skeleton className="h-4 rounded-full" style={{ width: `${width * 100}%` }} />
            <Skeleton className="h-4 w-16 rounded-full" />
          </span>
          <Skeleton className="h-2 w-full rounded-full" />
        </li>
      ))}
    </ul>
  )
}
