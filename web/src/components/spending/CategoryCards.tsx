/**
 * The month's money by category, as C-Gastos-Desktop draws it.
 *
 * <SpendingByCategory period />
 *   "Por categoría": every spending category with a total, largest first, each a
 *   CategoryBar linking to its movements for the month; the largest fills its
 *   track. "Sin categoría · N movimientos · Categorizar" goes last, as a to-do.
 *   "Ver reglas" in the header leads to Reglas, which categorise what repeats.
 *
 * <IncomeByCategory period />
 *   "Entradas de octubre" with the month's total at the right, and the same bars for
 *   Salario, Freelance and the rest. Money with no category reads "Sin categoría".
 *
 * Both lay the bars out in as many 280px-wide columns as fit (two on desktop, one on
 * a phone or beside the open assistant).
 */
import { Link } from 'react-router-dom'

import type { CategoryTotal } from '@/api/types'
import { Amount, CategoryBar, SectionCard } from '@/components/kit'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { categoryNames, UNCATEGORIZED_LABEL } from '@/lib/categories'
import { movementsHref, RULES_PATH } from '@/lib/nav'
import { useCategories, useCategoryTotals } from '@/lib/queries'
import { LoadError } from './LoadError'
import { monthInSentence, type SpendingPeriod } from './period'

const GRID =
  'grid grid-cols-[repeat(auto-fit,minmax(min(17.5rem,100%),1fr))] gap-x-10 gap-y-2'

function useTotals(period: SpendingPeriod, kind: 'spend' | 'income') {
  const totals = useCategoryTotals({ from: period.from, to: period.to, kind })
  const categories = useCategories()
  const names = categoryNames(categories.data?.categories)
  const all = totals.data?.totals ?? []

  const named = all
    .filter((t): t is CategoryTotal & { category_id: string } =>
      Boolean(t.category_id && t.total.cents > 0),
    )
    .map((t) => ({ ...t, name: names.get(t.category_id) ?? 'Otra categoría' }))
    .sort((a, b) => b.total.cents - a.total.cents || a.name.localeCompare(b.name, 'es'))
  const uncategorized = all.find((t) => t.category_id === null && t.total.cents > 0)
  const sum = [...named, ...(uncategorized ? [uncategorized] : [])].reduce(
    (acc, t) => acc + t.total.cents,
    0,
  )

  return {
    named,
    uncategorized,
    sum,
    loading: totals.isPending || categories.isPending,
    // Without the names every bar would read "Otra categoría".
    failed: totals.isError || (categories.isError && !categories.data),
  }
}

function BarsSkeleton({ rows }: { rows: number }) {
  return (
    <div className={GRID}>
      {Array.from({ length: rows }, (_, i) => (
        <div key={i} className="flex min-h-13 flex-col justify-center gap-2 py-1">
          <Skeleton className="h-5 w-2/3" />
          <Skeleton className="h-2" />
        </div>
      ))}
    </div>
  )
}

function Empty({ children }: { children: string }) {
  return <p className="text-body text-pretty text-muted-foreground">{children}</p>
}

export function SpendingByCategory({ period }: { period: SpendingPeriod }) {
  const { named, uncategorized, loading, failed } = useTotals(period, 'spend')
  const range = { from: period.from, to: period.to }
  const max = named[0]?.total.cents ?? 0

  return (
    <SectionCard
      title="Por categoría"
      action={
        <Button asChild variant="ghost">
          <Link to={RULES_PATH}>Ver reglas</Link>
        </Button>
      }
    >
      {loading ? (
        <BarsSkeleton rows={6} />
      ) : failed ? (
        <LoadError />
      ) : named.length === 0 && !uncategorized ? (
        <Empty>{`No hay gastos en ${monthInSentence(period.month)}.`}</Empty>
      ) : (
        <div className="flex flex-col gap-4">
          {named.length > 0 && (
            <div className={GRID}>
              {named.map((t) => (
                <CategoryBar
                  key={t.category_id}
                  name={t.name}
                  amount={t.total}
                  max={max}
                  to={movementsHref({ category: t.category_id, ...range })}
                />
              ))}
            </div>
          )}
          {uncategorized && (
            <CategoryBar
              variant="uncategorized"
              amount={uncategorized.total}
              count={uncategorized.count}
              to={movementsHref({ uncategorized: true, ...range })}
            />
          )}
        </div>
      )}
    </SectionCard>
  )
}

export function IncomeByCategory({ period }: { period: SpendingPeriod }) {
  const { named, uncategorized, sum, loading, failed } = useTotals(period, 'income')
  const range = { from: period.from, to: period.to }
  const max = Math.max(named[0]?.total.cents ?? 0, uncategorized?.total.cents ?? 0)
  const month = monthInSentence(period.month)

  return (
    <SectionCard
      title={`Entradas de ${month}`}
      action={!loading && !failed && sum > 0 ? <Amount amount={sum} /> : undefined}
    >
      {loading ? (
        <BarsSkeleton rows={2} />
      ) : failed ? (
        <LoadError />
      ) : sum === 0 ? (
        <Empty>{`No hay entradas en ${month}.`}</Empty>
      ) : (
        <div className={GRID}>
          {named.map((t) => (
            <CategoryBar
              key={t.category_id}
              name={t.name}
              amount={t.total}
              max={max}
              to={movementsHref({ category: t.category_id, ...range })}
            />
          ))}
          {uncategorized && (
            <CategoryBar
              name={UNCATEGORIZED_LABEL}
              amount={uncategorized.total}
              max={max}
              // Only the money that came in: without the kind, Movimientos would
              // also list the month's uncategorised spending.
              to={movementsHref({ uncategorized: true, kind: 'income', ...range })}
            />
          )}
        </div>
      )}
    </SectionCard>
  )
}
