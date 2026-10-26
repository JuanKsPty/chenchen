/**
 * Gastos (C-Gastos-Desktop; the assistant panel beside it is the shell's).
 *
 * The month comes from ?mes (spendingHref / parseSpendingMonth); without it, or past
 * today, it is the current month. From the top:
 *   - the month picker "‹ Octubre 2026 ›": beside the h1 from lg, the first row of
 *     the page below lg (where the top bar already says "Gastos")
 *   - "Salidas de octubre": the total, a line comparing it with last month, and
 *     "Entradas y salidas" for the four months ending with this one
 *   - "Por categoría": every category by amount, "Sin categoría" last, "Ver reglas"
 *   - "Entradas de octubre" by category, and beside it (or under it) the transfers
 *     waiting for a yes or no, #transferencias
 *
 * One column below lg; 16px between cards on mobile, 24 from lg. With no accounts
 * yet, the page is the empty state with the import action.
 *
 * #transferencias appears only once its data loads, so the shell cannot scroll to it
 * on arrival: the page does, once everything above it has its final height.
 */
import { FileUpIcon } from 'lucide-react'
import { useEffect, useRef } from 'react'
import { useLocation, useSearchParams } from 'react-router-dom'

import { useImport } from '@/components/import/ImportProvider'
import { EmptyState, PageHeader } from '@/components/kit'
import { IncomeByCategory, SpendingByCategory } from '@/components/spending/CategoryCards'
import { MonthPicker } from '@/components/spending/MonthPicker'
import { OutflowCard } from '@/components/spending/OutflowCard'
import { spendingPeriod, type SpendingPeriod } from '@/components/spending/period'
import {
  TRANSFERS_ID,
  TransferSuggestions,
} from '@/components/spending/TransferSuggestions'
import { monthBounds, shiftMonth } from '@/lib/format'
import { parseSpendingMonth } from '@/lib/nav'
import {
  useAccounts,
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
  const wantsTransfers = useLocation().hash === `#${TRANSFERS_ID}`

  useScrollToTransfers(wantsTransfers, period)

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
      <SpendingByCategory period={period} />

      <div className="grid grid-cols-[repeat(auto-fit,minmax(min(22rem,100%),1fr))] items-start gap-4 lg:gap-6">
        <IncomeByCategory period={period} />
        <TransferSuggestions keepWhenEmpty={wantsTransfers} />
      </div>
    </div>
  )
}

/**
 * Scrolls #transferencias into view once, 16px under whichever bar shows (as the
 * shell does for targets present on arrival), after the cards above it have loaded
 * so their skeletons cannot push it away again.
 */
function useScrollToTransfers(wanted: boolean, period: SpendingPeriod) {
  const done = useRef(false)
  const suggestions = useTransferSuggestions()
  const categories = useCategories()
  const flow = useFlow({
    from: monthBounds(shiftMonth(period.month, -3)).from,
    to: period.to,
    granularity: 'month',
  })
  const spend = useCategoryTotals({ from: period.from, to: period.to, kind: 'spend' })
  const income = useCategoryTotals({ from: period.from, to: period.to, kind: 'income' })
  const ready = [suggestions, categories, flow, spend, income].every((q) => !q.isPending)

  useEffect(() => {
    if (!wanted || !ready || done.current) return
    const target = document.getElementById(TRANSFERS_ID)
    if (!target) return
    const frame = requestAnimationFrame(() => {
      done.current = true
      const bars = document.querySelectorAll<HTMLElement>('[data-shell-bar]')
      const covered = Math.max(0, ...Array.from(bars, (bar) => bar.offsetHeight))
      const top = target.getBoundingClientRect().top + window.scrollY - covered - 16
      window.scrollTo(0, Math.max(0, top))
    })
    return () => cancelAnimationFrame(frame)
  }, [wanted, ready])
}
