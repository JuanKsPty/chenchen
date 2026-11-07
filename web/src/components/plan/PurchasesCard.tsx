/**
 * <PurchasesCard plan onAdd? onOpen className? />
 *
 *   onAdd   "Planear compra", the card's ghost action; left out when the month can no
 *           longer be edited
 *   onOpen  tapping a purchase, with its row: its sheet (which movement bought it)
 *           or, next month, its dialog; the row is where focus goes back to
 *
 * "Compras planeadas", #planeadas. One row per purchase the month shows: its name,
 * its price, and where it stands (purchaseStatus): "Marzo 2027 · apartas $220.00 al
 * mes", "Este mes · $1,100.00 apartados en el plan", "La compraste el 12 mar ·
 * $1,050.00", or, plainly, "No la compraste en marzo". One set aside a little each
 * month has a small track under it, "Apartado en el plan: $440.00 de $1,100.00": what
 * the plan has taken off what is free for it so far, this month included. A figure of
 * the plan, never a balance: no account is said to hold it. The track fills in the
 * accent, like any plan figure under what it was planned against; it can never pass
 * the price.
 */
import { ShoppingBagIcon } from 'lucide-react'

import type { MonthPlan, PlanPurchaseLine } from '@/api/types'
import { Amount, PlanTrack, SectionCard } from '@/components/kit'
import { Button } from '@/components/ui/button'
import { PLAN_SECTIONS } from '@/lib/nav'
import { PLAN_LIST, PlanRow } from './PlanRow'
import { setAsideSoFar } from './shares'
import { monthWord, purchaseStatus } from './words'

export function PurchasesCard({
  plan,
  onAdd,
  onOpen,
  className,
}: {
  plan: MonthPlan
  onAdd?: () => void
  onOpen: (line: PlanPurchaseLine, opener: HTMLElement) => void
  className?: string
}) {
  return (
    <SectionCard
      id={PLAN_SECTIONS.purchases}
      title="Compras planeadas"
      className={className}
      action={
        onAdd && (
          <Button variant="ghost" onClick={onAdd}>
            Planear compra
          </Button>
        )
      }
    >
      {plan.purchases.length === 0 ? (
        <p className="text-body text-pretty text-muted-foreground">
          {onAdd
            ? '¿Algo grande en camino? Plánealo y apartamos un poco cada mes.'
            : `No tenías compras planeadas en ${monthWord(plan.month)}.`}
        </p>
      ) : (
        <ul className={PLAN_LIST}>
          {plan.purchases.map((line) => (
            <li key={line.id}>
              <PlanRow
                icon={ShoppingBagIcon}
                title={line.name}
                subtitle={purchaseStatus(line, plan)}
                amount={<Amount amount={line.amount} />}
                onClick={(event) => onOpen(line, event.currentTarget)}
                footer={
                  line.status === 'setting_aside' && line.funding === 'monthly' ? (
                    <SetAside line={line} />
                  ) : undefined
                }
              />
            </li>
          ))}
        </ul>
      )}
    </SectionCard>
  )
}

/** What the plan has set aside for it so far, against its price. */
function SetAside({ line }: { line: PlanPurchaseLine }) {
  const soFar = setAsideSoFar(line)
  return (
    <>
      <PlanTrack amount={soFar} cap={line.amount} size="sm" />
      <span className="text-small font-medium text-pretty text-muted-foreground">
        Apartado en el plan: <Amount amount={soFar} size="inline" /> de{' '}
        <Amount amount={line.amount} size="inline" />
      </span>
    </>
  )
}
