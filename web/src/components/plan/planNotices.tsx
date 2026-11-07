/**
 * usePlanNotices(accounts)
 *
 *   accounts  the account list, to name a card that is due
 *
 * Inicio before the first import does not call it (HomeNotices), so neither the plan
 * nor the categories are asked for there.
 *
 * What this month's plan has to say on Inicio: at most two pending notices, which
 * HomeNotices puts after the purchases. They come from the server's `alerts` (the
 * web and Claude say the same thing) and, most important first:
 *   1. what is free, past it: "Lo libre de noviembre ya se usó" · "Mira tu plan y
 *      ajústalo si algo cambió." [Ver plan]
 *   2. caps past theirs: "Alimentación va por encima de su tope", or "2 categorías
 *      van por encima de su tope" [Ver topes]
 *   3. what is free, at 80%: "Queda poco libre en noviembre" [Ver plan]
 *   4. caps at 80%: "Alimentación está cerca de su tope", "2 categorías están cerca
 *      de su tope" [Ver topes]
 *   5. payments due within three days, a card's included: "Un pago sale pronto" ·
 *      "Alquiler sale el 18 nov.", "2 pagos salen pronto", or "Tu tarjeta vence
 *      pronto" when a card is the only one [Ver pagos]
 *
 * Each speaks only while something in it rose past what this device last saw
 * (plan/seen), and then says the month as it stands: one new cap past its own makes
 * "2 categorías van por encima de su tope" if another already was. Opening the plan
 * marks all of it seen, and so does each notice's action on its way there.
 *
 * Counts, names and days only, never amounts: Inicio shows at most five figures
 * before its first list. All in the pending tone, never orange: spending more than
 * planned is not something wrong, and nothing here scolds or warns. Until the plan
 * loads, when it fails, or while a cap's name is still on its way, there is nothing
 * to say. Nor is there for a month with no plan: the server still names a card due
 * soon then, but every notice here leads to a section of the plan, and an unplanned
 * month's page is only "Arma tu plan del mes". PlanGlance keeps the same silence.
 */
import { CalendarClockIcon, GaugeIcon } from 'lucide-react'
import { useMemo, type ReactNode } from 'react'

import type { Account, MonthPlan, PlanAlert } from '@/api/types'
import { Notice, type KitAction } from '@/components/kit'
import { shortAccountLabel } from '@/lib/account'
import { categoryNames } from '@/lib/categories'
import { formatCivilDayMonth } from '@/lib/format'
import { PLAN_CAPS_HREF, PLAN_PATH, PLAN_PAYMENTS_HREF } from '@/lib/nav'
import { useCategories } from '@/lib/queries'
import { alertSubject, markPlanSeen, useUnseenPlanAlerts } from './seen'
import { usePlan } from './usePlan'
import { monthWord } from './words'

const SHOWN = 2

type CapAlert = Extract<PlanAlert, { kind: 'cap' }>
type PaymentAlert = Extract<PlanAlert, { kind: 'payment' }>

export interface PlanNotice {
  key: string
  notice: ReactNode
}

export function usePlanNotices(accounts: Account[]): PlanNotice[] {
  const plan = usePlan()
  const categories = useCategories()
  const data = plan.data
  const unseen = useUnseenPlanAlerts(data)
  const tree = categories.data?.categories
  const names = useMemo(() => categoryNames(tree), [tree])

  if (!data?.planned || data.state !== 'current' || unseen.size === 0) return []
  // A cap's notice names its category: better a moment later than "Una categoría"
  // renamed under the owner's eyes.
  if (categories.isPending && data.alerts.some((a) => a.kind === 'cap')) return []

  const month = monthWord(data.month)
  const seen = () => markPlanSeen(data)
  const toPlan: KitAction = {
    label: 'Ver plan',
    ariaLabel: 'Ver plan del mes',
    to: PLAN_PATH,
    onClick: seen,
  }
  const toCaps: KitAction = {
    label: 'Ver topes',
    ariaLabel: 'Ver topes del plan',
    to: PLAN_CAPS_HREF,
    onClick: seen,
  }

  const free = data.alerts.filter((a) => a.kind === 'free')
  const caps = data.alerts.filter((a): a is CapAlert => a.kind === 'cap')
  const payments = data.alerts.filter((a): a is PaymentAlert => a.kind === 'payment')
  const capsOver = caps.filter((a) => a.level === 'over')
  const capsNear = caps.filter((a) => a.level === 'near')

  const candidates: { key: string; alerts: PlanAlert[]; notice: () => ReactNode }[] = [
    {
      key: 'plan-libre',
      alerts: free.filter((a) => a.level === 'over'),
      notice: () => (
        <Notice
          tone="pending"
          icon={GaugeIcon}
          title={`Lo libre de ${month} ya se usó`}
          description="Mira tu plan y ajústalo si algo cambió."
          action={toPlan}
        />
      ),
    },
    {
      key: 'plan-topes-encima',
      alerts: capsOver,
      notice: () => (
        <Notice
          tone="pending"
          icon={GaugeIcon}
          title={
            capsOver.length === 1
              ? `${capName(capsOver[0], names)} va por encima de su tope`
              : `${capsOver.length} categorías van por encima de su tope`
          }
          description={
            capsOver.length === 1
              ? 'Mira en qué se fue o ajusta el tope.'
              : 'Mira en qué se fue o ajusta los topes.'
          }
          action={toCaps}
        />
      ),
    },
    {
      key: 'plan-libre',
      alerts: free.filter((a) => a.level === 'near'),
      notice: () => (
        <Notice
          tone="pending"
          icon={GaugeIcon}
          title={`Queda poco libre en ${month}`}
          description="Mira cuánto te queda por día."
          action={toPlan}
        />
      ),
    },
    {
      key: 'plan-topes-cerca',
      alerts: capsNear,
      notice: () => (
        <Notice
          tone="pending"
          icon={GaugeIcon}
          title={
            capsNear.length === 1
              ? `${capName(capsNear[0], names)} está cerca de su tope`
              : `${capsNear.length} categorías están cerca de su tope`
          }
          description={
            capsNear.length === 1 ? 'Mira cuánto le queda.' : 'Mira cuánto les queda.'
          }
          action={toCaps}
        />
      ),
    },
    {
      key: 'plan-pagos',
      alerts: payments,
      notice: () => {
        const words = paymentWords(payments, data, accounts)
        return (
          <Notice
            tone="pending"
            icon={CalendarClockIcon}
            title={words.title}
            description={words.description}
            action={{
              label: 'Ver pagos',
              ariaLabel: 'Ver pagos del plan',
              to: PLAN_PAYMENTS_HREF,
              onClick: seen,
            }}
          />
        )
      },
    },
  ]

  return candidates
    .filter((c) => c.alerts.some((a) => unseen.has(alertSubject(a))))
    .slice(0, SHOWN)
    .map((c) => ({ key: c.key, notice: c.notice() }))
}

/** The category's name, "Padre › Hija" for a child; a cap is never left unnamed. */
function capName(alert: CapAlert | undefined, names: Map<string, string>): string {
  return (alert && names.get(alert.category_id)) ?? 'Una categoría'
}

/**
 * One payment is named, with its day: a fixed payment by the owner's own name for
 * it, a card by its alias or its label, as Pagos fijos names them. More than one is
 * a count; the plan says which.
 */
function paymentWords(
  alerts: PaymentAlert[],
  plan: MonthPlan,
  accounts: Account[],
): { title: string; description: string } {
  const [alert] = alerts
  if (alerts.length !== 1 || !alert)
    return {
      title: `${alerts.length} pagos salen pronto`,
      description: 'Mira cuáles y qué día.',
    }
  const day =
    alert.due_on === plan.today ? 'hoy' : `el ${formatCivilDayMonth(alert.due_on)}`
  if ('item_id' in alert) {
    const name = plan.fixed.find((line) => line.id === alert.item_id)?.name
    return {
      title: 'Un pago sale pronto',
      description: name ? `${name} sale ${day}.` : `Sale ${day}.`,
    }
  }
  const account = accounts.find((a) => a.id === alert.account_id)
  const name = account ? account.alias?.trim() || shortAccountLabel(account) : null
  return {
    title: 'Tu tarjeta vence pronto',
    description: name ? `${name} vence ${day}.` : `Vence ${day}.`,
  }
}
