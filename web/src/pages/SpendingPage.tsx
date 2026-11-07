/**
 * Gastos (C-Gastos-Desktop; the assistant panel beside it is the shell's).
 *
 * The month comes from ?mes (spendingHref / parseSpendingMonth); without it, or past
 * today, it is the current month. From the top:
 *   - the month picker "‹ Octubre 2026 ›": beside the h1 from lg, the first row of
 *     the page below lg (where the top bar already says "Gastos")
 *   - "Salidas de octubre": the total, a line comparing it with last month, and
 *     "Entradas y salidas" for the four months ending with this one
 *   - "Plan de octubre": what is left of what the month's plan left free, and the
 *     way into it (or, this month with no plan, an invite to make one)
 *   - "Por categoría": every category by amount, "Sin categoría" last, "Ver reglas"
 *   - "Entradas de octubre" by category, and beside it (or under it) the transfers
 *     waiting for a yes or no, #transferencias, and the purchases the owner noted that
 *     their statement does not confirm, #compras
 *
 * One column below lg; 16px between cards on mobile, 24 from lg. With no accounts
 * yet, the page is the empty state with the import action.
 *
 * #transferencias and #compras appear only once their data loads, so the shell cannot
 * scroll to them on arrival: the page does, once everything above has its final height.
 */
import { FileUpIcon } from 'lucide-react'
import { useEffect, useRef } from 'react'
import { useLocation, useSearchParams } from 'react-router-dom'

import { useImport } from '@/components/import/ImportProvider'
import { EmptyState, PageHeader } from '@/components/kit'
import { usePlan } from '@/components/plan/usePlan'
import { CAPTURES_ID, CaptureReview } from '@/components/spending/CaptureReview'
import { IncomeByCategory, SpendingByCategory } from '@/components/spending/CategoryCards'
import { MonthPicker } from '@/components/spending/MonthPicker'
import { OutflowCard } from '@/components/spending/OutflowCard'
import { spendingPeriod, type SpendingPeriod } from '@/components/spending/period'
import { PlanCard } from '@/components/spending/PlanCard'
import {
  TRANSFERS_ID,
  TransferSuggestions,
} from '@/components/spending/TransferSuggestions'
import { monthBounds, shiftMonth } from '@/lib/format'
import { parseSpendingMonth } from '@/lib/nav'
import {
  useAccounts,
  useCaptureReview,
  useCategories,
  useCategoryTotals,
  useFlow,
  useTransferSuggestions,
} from '@/lib/queries'
import { useDocumentTitle } from '@/lib/title'

export function SpendingPage() {
  useDocumentTitle('Gastos')
  const [params] = useSearchParams()
  const period = spendingPeriod(parseSpendingMonth(params))
  const accounts = useAccounts()
  const { openImport } = useImport()
  const hash = useLocation().hash
  const wantsTransfers = hash === `#${TRANSFERS_ID}`
  const wantsCaptures = hash === `#${CAPTURES_ID}`

  useScrollToSection(
    wantsTransfers ? TRANSFERS_ID : wantsCaptures ? CAPTURES_ID : null,
    period,
  )

  if (accounts.data && accounts.data.accounts.length === 0) {
    return (
      <div className="flex flex-col gap-4 lg:gap-6">
        <PageHeader title="Gastos" />
        <EmptyState
          framed
          headingLevel={2}
          illustration="import"
          title="Aquí verás en qué se te va"
          description="Importa tu primer estado de cuenta y te lo dejamos ordenadito por categoría."
          action={{
            label: 'Importar estado de cuenta',
            icon: FileUpIcon,
            onClick: () => openImport(),
          }}
        />
      </div>
    )
  }

  return (
    <div className="flex flex-col gap-4 lg:gap-6">
      <PageHeader
        title="Gastos"
        className="max-lg:sr-only"
        actions={<MonthPicker month={period.month} className="hidden lg:flex" />}
      />
      <MonthPicker
        month={period.month}
        size="wide"
        className="w-full md:max-w-sm md:self-start lg:hidden"
      />

      <OutflowCard period={period} />
      <PlanCard period={period} />
      <SpendingByCategory period={period} />

      <div className="grid grid-cols-[repeat(auto-fit,minmax(min(22rem,100%),1fr))] items-start gap-4 lg:gap-6">
        <IncomeByCategory period={period} />
        <TransferSuggestions keepWhenEmpty={wantsTransfers} />
        <CaptureReview keepWhenEmpty={wantsCaptures} />
      </div>
    </div>
  )
}

/**
 * Scrolls #transferencias or #compras (`id`; null for neither) into view once, 16px
 * under whichever bar shows (as the shell does for targets present on arrival), after
 * every card around it has loaded so their skeletons cannot push it away again.
 */
function useScrollToSection(id: string | null, period: SpendingPeriod) {
  const done = useRef(false)
  const suggestions = useTransferSuggestions()
  const review = useCaptureReview()
  const categories = useCategories()
  const flow = useFlow({
    from: monthBounds(shiftMonth(period.month, -3)).from,
    to: period.to,
    granularity: 'month',
  })
  const spend = useCategoryTotals({ from: period.from, to: period.to, kind: 'spend' })
  const income = useCategoryTotals({ from: period.from, to: period.to, kind: 'income' })
  const plan = usePlan(period.current ? undefined : period.month)
  const ready = [suggestions, review, categories, flow, spend, income, plan].every(
    (q) => !q.isPending,
  )

  useEffect(() => {
    if (!id || !ready || done.current) return
    const target = document.getElementById(id)
    if (!target) return
    const frame = requestAnimationFrame(() => {
      done.current = true
      const bars = document.querySelectorAll<HTMLElement>('[data-shell-bar]')
      const covered = Math.max(0, ...Array.from(bars, (bar) => bar.offsetHeight))
      const top = target.getBoundingClientRect().top + window.scrollY - covered - 16
      window.scrollTo(0, Math.max(0, top))
    })
    return () => cancelAnimationFrame(frame)
  }, [id, ready])
}
