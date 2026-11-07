/**
 * <FixedCard plan accounts onAdd? onOpen className? />
 *
 *   accounts  the owner's accounts, to name the cards
 *   onAdd     "Agregar pago fijo", the card's ghost action; left out when the month
 *             can no longer be edited
 *   onOpen    tapping a payment, with its row: its sheet (which movement paid it) or,
 *             next month, its dialog; the row is where focus goes back to
 *
 * "Pagos fijos", #pagos. One row per payment of the month: its name, where it
 * stands ("Salió el 04 nov", "Sale el 18 nov", "Aún no lo vemos · era el 05 nov")
 * and what it takes: what went out once it did, what was planned until then. Then
 * "Ya salieron $39.30 de $520.00."
 *
 * Under them, in the current month, "Tarjetas": each card's due date from its
 * latest statement ("Vence el 15 nov", or "Ya la pagaste"), only to be read. Paying
 * a card moves money between the owner's accounts; what it bought already counts as
 * spending, so it never comes off what is free, and the card says so.
 */
import { CreditCardIcon, RepeatIcon } from 'lucide-react'

import type { Account, MonthPlan, PlanFixedLine } from '@/api/types'
import { Amount, SectionCard } from '@/components/kit'
import { Button } from '@/components/ui/button'
import { shortAccountLabel } from '@/lib/account'
import { formatCivilDayMonth, money } from '@/lib/format'
import { PLAN_SECTIONS } from '@/lib/nav'
import { PLAN_LIST, PlanRow } from './PlanRow'
import { fixedStatus, monthWord } from './words'

export function FixedCard({
  plan,
  accounts,
  onAdd,
  onOpen,
  className,
}: {
  plan: MonthPlan
  accounts: Account[] | undefined
  onAdd?: () => void
  onOpen: (line: PlanFixedLine, opener: HTMLElement) => void
  className?: string
}) {
  const fixed = plan.fixed
  // What has gone out of what the payments take: what each paid, or, for one paid
  // from elsewhere, what was planned for it.
  const out = fixed.reduce(
    (sum, f) => sum + (f.paid?.cents ?? (f.marked_paid ? f.amount.cents : 0)),
    0,
  )
  const cards = plan.state === 'current' ? plan.cards : []
  const byId = new Map((accounts ?? []).map((a) => [a.id, a]))

  return (
    <SectionCard
      id={PLAN_SECTIONS.payments}
      title="Pagos fijos"
      className={className}
      action={
        onAdd && (
          <Button variant="ghost" onClick={onAdd}>
            Agregar pago fijo
          </Button>
        )
      }
    >
      <div className="flex flex-col gap-4">
        {fixed.length === 0 ? (
          <p className="text-body text-pretty text-muted-foreground">
            {onAdd
              ? 'Agrega lo que sale solo cada mes: alquiler, luz, suscripciones.'
              : `No tenías pagos fijos en ${monthWord(plan.month)}.`}
          </p>
        ) : (
          <>
            <ul className={PLAN_LIST}>
              {fixed.map((line) => (
                <li key={line.id}>
                  <PlanRow
                    icon={RepeatIcon}
                    title={line.name}
                    subtitle={fixedStatus(line, plan)}
                    amount={<Amount amount={line.paid ?? line.amount} />}
                    onClick={(event) => onOpen(line, event.currentTarget)}
                  />
                </li>
              ))}
            </ul>
            {plan.state !== 'future' && (
              <p className="text-small font-medium text-pretty text-muted-foreground">
                {plan.state === 'past' ? 'Salieron ' : 'Ya salieron '}
                <Amount amount={out} size="inline" /> de{' '}
                <Amount amount={plan.summary.fixed} size="inline" />.
              </p>
            )}
          </>
        )}

        {cards.length > 0 && (
          <div className="flex flex-col gap-2">
            <h3 className="text-small font-semibold">Tarjetas</h3>
            <ul className={PLAN_LIST}>
              {cards.map((card) => {
                const account = byId.get(card.account_id)
                const name = account
                  ? account.alias?.trim() || shortAccountLabel(account)
                  : 'Tarjeta'
                let when = 'Ya la pagaste'
                if (!card.paid) {
                  when = `Vence el ${formatCivilDayMonth(card.due_on)}`
                  if (card.minimum_payment)
                    when += ` · mínimo ${money(card.minimum_payment)}`
                }
                return (
                  <li key={card.account_id}>
                    <PlanRow icon={CreditCardIcon} title={name} subtitle={when} />
                  </li>
                )
              })}
            </ul>
            <p className="text-small font-medium text-pretty text-muted-foreground">
              No resta de lo libre: sus compras ya cuentan.
            </p>
          </div>
        )}
      </div>
    </SectionCard>
  )
}
