/**
 * <PurchaseSheet plan line open onOpenChange onEdit />
 *
 *   line    the planned purchase in the month, as the plan read it
 *   onEdit  "Editar compra" (only while the month can still be edited and the
 *           purchase is due in it or later): the sheet closes and the purchase's
 *           dialog opens
 *
 * What a tapped planned purchase opens: which movement bought it. The plan recognises
 * a purchase by itself only when exactly one outflow near its price could be it; with
 * two, or with none that reads like it, it waits for the owner to say. Until then the
 * plan keeps setting its share aside, and the outflow counts in what went out of what
 * is free.
 *
 * While it is not bought, "¿Cuál fue?": the outflows the plan offers (candidates, from
 * any month it can be bought in, closest to its price first), then the rest of the
 * month's spending. Picking one says it bought it, in the month that movement was
 * booked (usePlanLinks' assign with that month), whichever month is on screen. Once
 * bought, the movements that bought it, each with "No es esta", which says that
 * movement was not it, whoever had picked it. There is no "paid from elsewhere": a
 * purchase is bought by a movement, and it cost what the movement says.
 *
 * A past month can still be linked (it is how the month gets right) but not edited;
 * next month opens its dialog instead (PlanPage), since nothing was bought yet. A
 * purchase bought in the month after it was due cannot be edited from that month
 * either: the server plans one from the month on screen to the month it is due, and
 * that month is behind. Each answer closes the sheet with a toast whose "Deshacer"
 * takes it back; a refusal stays in the sheet as an Alert.
 */
import { useId, useState } from 'react'

import type { MonthPlan, PlanPurchaseLine } from '@/api/types'
import { ResponsiveDialogContent } from '@/components/accounts/ResponsiveDialog'
import { Button } from '@/components/ui/button'
import { Dialog } from '@/components/ui/dialog'
import { useReturnFocus } from '@/lib/focus'
import { money } from '@/lib/format'
import { FormFailure, useOpenRound } from './forms'
import {
  MonthMovements,
  MovementChoice,
  PaidMovements,
  type Choice,
} from './MovementPicker'
import { usePlanLinks } from './usePlan'
import { purchaseStatus } from './words'

export function PurchaseSheet({
  plan,
  line,
  open,
  onOpenChange,
  onEdit,
}: {
  plan: MonthPlan
  line: PlanPurchaseLine | null
  open: boolean
  onOpenChange: (open: boolean) => void
  onEdit: (line: PlanPurchaseLine) => void
}) {
  const returnFocus = useReturnFocus()
  const round = useOpenRound(open)
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      {line && (
        <SheetBody
          key={round}
          plan={plan}
          line={line}
          returnFocus={returnFocus}
          onDone={() => onOpenChange(false)}
          onEdit={() => {
            onOpenChange(false)
            onEdit(line)
          }}
        />
      )}
    </Dialog>
  )
}

/** Whether the purchase can be edited from the month on screen. */
function purchaseEditable(plan: MonthPlan, line: PlanPurchaseLine): boolean {
  return plan.editable && line.target_month >= plan.month
}

function SheetBody({
  plan,
  line,
  returnFocus,
  onDone,
  onEdit,
}: {
  plan: MonthPlan
  line: PlanPurchaseLine
  returnFocus: ReturnType<typeof useReturnFocus>
  onDone: () => void
  onEdit: () => void
}) {
  const [failure, setFailure] = useState<string | null>(null)
  const links = usePlanLinks({ item_id: line.id, month: plan.month }, onDone, setFailure)
  const pickId = useId()
  const busy = links.busy
  const bought = line.status === 'bought'

  // Every movement that already has a place: the month's payments and purchases, and
  // the ones offered above the month's list.
  const placed = new Set<string>([
    ...plan.fixed.flatMap((f) => f.movements.map((m) => m.id)),
    ...plan.purchases.flatMap((p) => p.movements.map((m) => m.id)),
    ...line.candidates.map((c) => c.id),
  ])

  // It was bought in the month its movement was booked, which a candidate can be from.
  const assign = (choice: Choice) =>
    links.assign(
      choice.id,
      `Listo: ese movimiento es ${line.name}.`,
      choice.booked_on.slice(0, 7),
    )

  return (
    <ResponsiveDialogContent
      {...returnFocus}
      title={line.name}
      description={`${money(line.amount)} · ${purchaseStatus(line, plan)}`}
      footer={
        purchaseEditable(plan, line) && (
          <Button variant="secondary" size="lg" className="lg:h-11" onClick={onEdit}>
            Editar compra
          </Button>
        )
      }
    >
      {!bought && (
        <section aria-labelledby={pickId} className="flex flex-col gap-2">
          <div className="flex flex-col gap-1">
            <h3 id={pickId} className="text-body font-semibold">
              ¿Cuál fue?
            </h3>
            {line.status === 'setting_aside' && (
              <p className="text-small font-medium text-pretty text-muted-foreground">
                Si ya la compraste, elige con qué movimiento.
              </p>
            )}
          </div>
          {line.candidates.length > 0 && (
            <ul className="-mx-3 flex flex-col">
              {line.candidates.map((c) => (
                <li key={c.id}>
                  <MovementChoice
                    movement={c}
                    busy={busy}
                    onPick={assign}
                    pick="Fue esta"
                  />
                </li>
              ))}
            </ul>
          )}
          <MonthMovements
            month={plan.month}
            side="out"
            planned={line.amount.cents}
            placed={placed}
            busy={busy}
            onPick={assign}
            pick="Fue esta"
          />
        </section>
      )}

      <PaidMovements
        title="La compraste con"
        movements={line.movements}
        busy={busy}
        reject="No es esta"
        onNotThis={(movement) =>
          links.notThis(movement, `Listo: ese movimiento no es ${line.name}.`)
        }
      />

      {failure && <FormFailure title="No pudimos guardarlo" message={failure} />}
    </ResponsiveDialogContent>
  )
}
