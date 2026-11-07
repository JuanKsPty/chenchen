/**
 * Plan del mes (/gastos/plan): what is free to spend this month, and what it comes
 * from. Under Gastos, like Reglas: the mobile bar shows "Plan del mes" with a chevron
 * back to Gastos, and the Gastos tab stays the active one. No action in the bar and
 * no primary on the page: every edit opens a dialog that carries its own (the
 * desktop bar's "Importar estado" stays the view's one), except the first time,
 * when "Armar mi plan" is the page's only one.
 *
 * The month comes from ?mes (planHref; parseSpendingMonth reads it), from any past
 * month up to next month, which can already be planned; anything later is this
 * month. This month is asked for without a month, so the server, on Panama time,
 * says which month that is, and Gastos' card shares the answer. From the top:
 *   - the month picker, "›" up to next month
 *   - for next month "Así se ve diciembre. Lo que cambies aquí vale desde
 *     diciembre."; for a past one "Así cerró octubre." (read only, except for saying
 *     which movement paid a payment)
 *   - "1 transferencia por confirmar · Mientras tanto, cuenta como gasto.": a card
 *     payment not yet paired counts as spending, so it is said before the figures
 *   - PlanHero: "Te quedan", the figure, the quincenas and the account, with each
 *     paycheck and where it stands
 *   - Topes por categoría (#topes), Pagos fijos (#pagos), Ahorro (#ahorro)
 *
 * A paycheck or a payment opens its sheet: which movement it was, or that it was
 * paid outside, and taking either back; a past month too, since that is how it gets
 * right. Next month, where nothing has gone out yet, a payment opens its dialog.
 * "Editar" in a sheet closes it and opens the dialog, and focus then goes back to the
 * row that opened the sheet: the sheet's own button is gone by the time the dialog
 * closes.
 *
 * From 56rem of page (not of window: the assistant panel takes room) the hero and
 * the caps take 7 of 12 columns and the rest 5, each column flowing on its own;
 * narrower, one column in the same order, which is also the phone's reading order.
 *
 * With nothing planned yet: "Arma tu plan del mes", whose "Armar mi plan" opens the
 * setup (PlanSetupDialog): what chenchén found in the history, in three steps. So
 * does ?armar=1, Gastos' invite, once the plan says there is nothing planned; the
 * address then drops it, so a reload or "Atrás" never opens it again. Once there is
 * a plan, this month's cards offer what else the history suggests: "¿También son
 * fijos?", "¿También es un sueldo?", "¿También es ahorro?". The sections appear once
 * their data does, so the page, not the shell, scrolls to #topes, #pagos or #ahorro.
 *
 * Opening this month's plan marks what it has to say (near or past what is free or a
 * cap, payments due soon) as seen on this device (plan/seen), so Inicio's notices
 * about it wait until something rises again.
 */
import { ArrowLeftRightIcon, FileUpIcon } from 'lucide-react'
import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { useLocation, useSearchParams } from 'react-router-dom'

import type {
  PlanCapLine,
  PlanFixedLine,
  PlanIncomeLine,
  PlanSavingLine,
} from '@/api/types'
import { useImport } from '@/components/import/ImportProvider'
import { EmptyState, Notice, PageHeader } from '@/components/kit'
import { CapDialog } from '@/components/plan/CapDialog'
import { CapsCard } from '@/components/plan/CapsCard'
import { FixedCard } from '@/components/plan/FixedCard'
import { FixedPaymentDialog } from '@/components/plan/FixedPaymentDialog'
import { FixedPaymentSheet } from '@/components/plan/FixedPaymentSheet'
import { GoalDialog } from '@/components/plan/GoalDialog'
import { IncomeDialog } from '@/components/plan/IncomeDialog'
import { IncomeSheet } from '@/components/plan/IncomeSheet'
import { planMonth } from '@/components/plan/month'
import { PlanHero } from '@/components/plan/PlanHero'
import { PlanSetupDialog } from '@/components/plan/PlanSetupDialog'
import { SavingsCard } from '@/components/plan/SavingsCard'
import { markPlanSeen } from '@/components/plan/seen'
import { usePlan, usePlanSuggestions } from '@/components/plan/usePlan'
import { monthWord } from '@/components/plan/words'
import { problemMessage } from '@/components/settings/problem'
import { useTopBar } from '@/components/shell/TopBar'
import { MonthPicker } from '@/components/spending/MonthPicker'
import { Skeleton } from '@/components/ui/skeleton'
import { categoryNames } from '@/lib/categories'
import {
  isPlanSetupRequested,
  parseSpendingMonth,
  PLAN_PATH,
  PLAN_SECTIONS,
  planHref,
  SPENDING_PATH,
  TRANSFERS_HREF,
} from '@/lib/nav'
import { useAccounts, useCategories, useTransferSuggestions } from '@/lib/queries'
import { useDocumentTitle } from '@/lib/title'

const TITLE = 'Plan del mes'

/** Which editor is open; the item it edits is kept apart, so it stays while it closes. */
type Open = 'setup' | 'income' | 'incomeSheet' | 'fixed' | 'sheet' | 'saving' | 'cap'

export function PlanPage() {
  useDocumentTitle(TITLE)
  useTopBar({ title: TITLE, back: SPENDING_PATH })
  const [params, setParams] = useSearchParams()
  const { month, current, last } = planMonth(parseSpendingMonth(params))
  const plan = usePlan(current ? undefined : month)
  const shown = plan.data
  // Seeing this month's plan is seeing all Inicio's notices would say about it.
  useEffect(() => {
    if (shown?.state === 'current') markPlanSeen(shown)
  }, [shown])
  const accounts = useAccounts()
  const categories = useCategories()
  const transfers = useTransferSuggestions()
  const tree = categories.data?.categories
  const names = useMemo(() => categoryNames(tree), [tree])
  const { openImport } = useImport()

  const hash = useLocation().hash
  const section = Object.values(PLAN_SECTIONS).find((id) => hash === `#${id}`) ?? null
  useScrollToSection(
    section,
    !plan.isPending && !categories.isPending && !accounts.isPending,
  )

  const [open, setOpen] = useState<Open | null>(null)
  const [fixed, setFixed] = useState<PlanFixedLine | null>(null)
  const [sheet, setSheet] = useState<PlanFixedLine | null>(null)
  const [paycheck, setPaycheck] = useState<PlanIncomeLine | null>(null)
  const [saving, setSaving] = useState<PlanSavingLine | null>(null)
  const [cap, setCap] = useState<PlanCapLine | null>(null)
  // The row that opened the sheet on screen, and, for a dialog a sheet opened, the
  // row focus goes back to when it closes (null for any other dialog).
  const sheetOpener = useRef<HTMLElement | null>(null)
  const handoff = useRef<HTMLElement | null>(null)
  // The hero's heading: where focus lands once the setup made the plan it heads.
  const heroHeading = useRef<HTMLHeadingElement>(null)

  /** Opens a dialog from the page itself: focus goes back to what opened it. */
  const openDialog = (next: Open) => {
    handoff.current = null
    setOpen(next)
  }
  /** Opens a sheet from its row. */
  const openSheet = (next: Open, opener: HTMLElement) => {
    sheetOpener.current = opener
    setOpen(next)
  }
  /** A sheet's "Editar": the sheet closes, its dialog opens, focus ends at the row. */
  const openFromSheet = (next: Open) => {
    handoff.current = sheetOpener.current
    setOpen(next)
  }

  const data = plan.data
  // Suggestions are offered by the setup (nothing planned yet, a month that can be
  // planned) and under this month's cards; any other month asks for none.
  const suggestions = usePlanSuggestions(
    data !== undefined && data.editable && (data.state === 'current' || !data.planned),
  )

  // ?armar=1 opens the setup on arrival, once the plan says there is nothing
  // planned to set up over, and leaves the address either way.
  const setupRequested = isPlanSetupRequested(params)
  useEffect(() => {
    if (!setupRequested || !data) return
    if (!data.planned && data.editable) setOpen('setup')
    setParams(
      (previous) => {
        const next = new URLSearchParams(previous)
        next.delete('armar')
        return next
      },
      { replace: true },
    )
  }, [setupRequested, data, setParams])

  const picker = (
    <MonthPicker month={month} max={last} href={planHref} className="hidden lg:flex" />
  )
  const header = (
    <>
      <PageHeader title={TITLE} className="max-lg:sr-only" actions={picker} />
      <MonthPicker
        month={month}
        max={last}
        href={planHref}
        size="wide"
        className="w-full md:max-w-sm md:self-start lg:hidden"
      />
    </>
  )

  if (plan.isPending) {
    return (
      <div className="flex flex-col gap-4 lg:gap-6">
        {header}
        <PlanSkeleton />
      </div>
    )
  }

  if (!data) {
    return (
      <div className="flex flex-col gap-4 lg:gap-6">
        {header}
        <EmptyState
          illustration={null}
          title="No pudimos cargar tu plan"
          description={problemMessage(plan.error)}
          secondaryAction={{ label: 'Reintentar', onClick: () => void plan.refetch() }}
        />
      </div>
    )
  }

  const editable = data.editable
  // What else the history suggests, under this month's cards while it can change.
  const offered = data.state === 'current' && editable ? suggestions.data : undefined
  const pending = transfers.data?.suggestions.length ?? 0
  const noAccounts = accounts.data !== undefined && accounts.data.accounts.length === 0

  const openFixed = (line: PlanFixedLine, opener: HTMLElement) => {
    if (data.state === 'future') {
      setFixed(line)
      openDialog('fixed')
    } else {
      setSheet(line)
      openSheet('sheet', opener)
    }
  }

  let body: ReactNode
  if (!data.planned && data.state === 'past') {
    body = (
      <EmptyState
        title={`No tenías plan en ${monthWord(data.month)}.`}
        secondaryAction={{ label: 'Ir a este mes', to: PLAN_PATH }}
      />
    )
  } else if (!data.planned) {
    body = (
      <EmptyState
        framed
        headingLevel={2}
        title="Arma tu plan del mes"
        description="Dinos cuánto te entra y qué pagas cada mes, y te decimos cuánto te queda libre por día."
        action={{ label: 'Armar mi plan', onClick: () => openDialog('setup') }}
        secondaryAction={
          noAccounts
            ? {
                label: 'Importar estado de cuenta',
                icon: FileUpIcon,
                onClick: () => openImport(),
              }
            : undefined
        }
      />
    )
  } else {
    body = (
      <div className="@container">
        <div className="grid gap-4 lg:gap-6 @4xl:grid-cols-12 @4xl:items-start">
          <div className="flex min-w-0 flex-col gap-4 lg:gap-6 @4xl:col-span-7">
            <PlanHero
              plan={data}
              onEditIncome={editable ? () => openDialog('income') : undefined}
              onOpenIncome={(line, opener) => {
                setPaycheck(line)
                openSheet('incomeSheet', opener)
              }}
              suggestions={offered?.income}
              headingRef={heroHeading}
            />
            <CapsCard
              plan={data}
              names={categories.data ? names : undefined}
              failed={categories.isError && !categories.data}
              onAdd={
                editable
                  ? () => {
                      setCap(null)
                      openDialog('cap')
                    }
                  : undefined
              }
              onEdit={
                editable
                  ? (line) => {
                      setCap(line)
                      openDialog('cap')
                    }
                  : undefined
              }
            />
          </div>
          <div className="flex min-w-0 flex-col gap-4 lg:gap-6 @4xl:col-span-5">
            <FixedCard
              plan={data}
              accounts={accounts.data?.accounts}
              onAdd={
                editable
                  ? () => {
                      setFixed(null)
                      openDialog('fixed')
                    }
                  : undefined
              }
              onOpen={openFixed}
              suggestions={offered?.fixed}
            />
            <SavingsCard
              plan={data}
              accounts={accounts.data?.accounts}
              onAdd={
                editable
                  ? () => {
                      setSaving(null)
                      openDialog('saving')
                    }
                  : undefined
              }
              onEdit={
                editable
                  ? (line) => {
                      setSaving(line)
                      openDialog('saving')
                    }
                  : undefined
              }
              suggestions={offered?.savings}
            />
          </div>
        </div>
      </div>
    )
  }

  const close = (next: boolean) => {
    if (!next) setOpen(null)
  }

  return (
    <div className="flex flex-col gap-4 lg:gap-6">
      {header}

      {data.planned && data.state !== 'current' && (
        <p className="text-body text-pretty text-muted-foreground">
          {data.state === 'future'
            ? `Así se ve ${monthWord(data.month)}. Lo que cambies aquí vale desde ${monthWord(data.month)}.`
            : `Así cerró ${monthWord(data.month)}.`}
        </p>
      )}

      {pending > 0 && data.state !== 'future' && (
        <Notice
          tone="pending"
          icon={ArrowLeftRightIcon}
          title={`${pending} ${pending === 1 ? 'transferencia' : 'transferencias'} por confirmar`}
          description="Mientras tanto, cuenta como gasto."
          action={{
            label: 'Revisar',
            ariaLabel: 'Revisar transferencias',
            to: TRANSFERS_HREF,
          }}
        />
      )}

      {body}

      <PlanSetupDialog
        plan={data}
        suggestions={suggestions.data}
        failed={suggestions.isError}
        open={open === 'setup'}
        onOpenChange={close}
        returnTo={heroHeading}
      />
      <IncomeDialog
        plan={data}
        open={open === 'income'}
        onOpenChange={close}
        returnTo={handoff}
      />
      <IncomeSheet
        plan={data}
        line={paycheck}
        open={open === 'incomeSheet'}
        onOpenChange={close}
        onEdit={() => openFromSheet('income')}
      />
      <FixedPaymentDialog
        plan={data}
        item={fixed}
        open={open === 'fixed'}
        onOpenChange={close}
        returnTo={handoff}
      />
      <FixedPaymentSheet
        plan={data}
        line={sheet}
        open={open === 'sheet'}
        onOpenChange={close}
        onEdit={(line) => {
          setFixed(line)
          openFromSheet('fixed')
        }}
      />
      <GoalDialog plan={data} item={saving} open={open === 'saving'} onOpenChange={close} />
      <CapDialog plan={data} cap={cap} open={open === 'cap'} onOpenChange={close} />
    </div>
  )
}

/**
 * Shaped like the page it stands in for: the hero with its figure and track, then a
 * card of rows. Never "$0.00": nothing is known yet.
 */
function PlanSkeleton() {
  return (
    <div className="flex flex-col gap-4 lg:gap-6" aria-hidden="true">
      <div className="flex flex-col gap-3 rounded-2xl bg-card p-4 shadow-card md:p-6">
        <Skeleton className="h-6 w-24" />
        <Skeleton className="h-13 w-56" />
        <Skeleton className="h-2" />
        <Skeleton className="h-4 w-48" />
        <Skeleton className="h-5 w-40" />
      </div>
      <div className="flex flex-col gap-1 rounded-2xl bg-card p-4 shadow-card md:p-6">
        <Skeleton className="mb-3 h-7 w-32" />
        {[0, 1, 2].map((row) => (
          <div key={row} className="flex min-h-16 items-center gap-3 md:min-h-14">
            <Skeleton className="size-10 shrink-0 rounded-full" />
            <div className="flex flex-1 flex-col gap-2">
              <Skeleton className="h-4 w-36" />
              <Skeleton className="h-3.5 w-28" />
            </div>
          </div>
        ))}
      </div>
    </div>
  )
}

/**
 * Scrolls #topes, #pagos or #ahorro (`id`; null for none) into view once, 16px under
 * whichever bar shows, after the plan and what names its rows have loaded, so no
 * skeleton can push the section away again. A copy of Gastos' own.
 */
function useScrollToSection(id: string | null, ready: boolean) {
  const done = useRef(false)

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
