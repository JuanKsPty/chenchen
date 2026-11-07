/**
 * <IncomeSheet plan line open onOpenChange onEdit />
 *
 *   line    the paycheck's month, as the plan read it
 *   onEdit  "Editar ingreso" (only while the month can still be edited): the sheet
 *           closes and the income dialog opens
 *
 * What a tapped paycheck opens: which deposit it was, said by the owner. The plan
 * recognises a paycheck by its text, its amount and its day; one saved without the
 * text, or a deposit that reads otherwise, is never recognised, and its money then
 * reads as income outside the plan, which the owner might add to what is free a
 * second time. Here they say which it was.
 *
 * While it has not come in:
 *   - "¿Cuál fue?": the deposits that come close (candidates), then the rest of the
 *     month's income, closest amount first. Picking one says it brought in this
 *     month of the paycheck.
 *   - "Me llegó a otra cuenta": it came in where chenchén does not see, an account
 *     it does not hold.
 * Once it came in, the deposits it came in as, each with "No es este", which says
 * that deposit was not it, whoever had picked it (usePlanLinks). Marked as come in
 * elsewhere, "Aún no me ha llegado" takes that back by its link; once a deposit is
 * recognised as it after all, "Quitar la marca" does, since the deposit says more.
 *
 * FixedPaymentSheet's twin: each answer closes the sheet with a toast whose
 * "Deshacer" takes it back; a refusal stays in the sheet as an Alert. A past month
 * can still be linked but not edited.
 */
import { useId, useState } from 'react'

import type { MonthPlan, PlanIncomeLine } from '@/api/types'
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
import { incomeStatus, monthWord } from './words'

export function IncomeSheet({
  plan,
  line,
  open,
  onOpenChange,
  onEdit,
}: {
  plan: MonthPlan
  line: PlanIncomeLine | null
  open: boolean
  onOpenChange: (open: boolean) => void
  onEdit: () => void
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
            onEdit()
          }}
        />
      )}
    </Dialog>
  )
}

function SheetBody({
  plan,
  line,
  returnFocus,
  onDone,
  onEdit,
}: {
  plan: MonthPlan
  line: PlanIncomeLine
  returnFocus: ReturnType<typeof useReturnFocus>
  onDone: () => void
  onEdit: () => void
}) {
  const [failure, setFailure] = useState<string | null>(null)
  const links = usePlanLinks({ item_id: line.id, month: plan.month }, onDone, setFailure)
  const pickId = useId()

  const busy = links.busy
  // The link by which the owner said it came in to another account, if they did.
  const mark = line.marked_paid ? line.paid_link_id : null
  const arrived = line.movements.length > 0

  // Every deposit that already has a place: the month's paychecks, and the ones
  // offered above the month's list.
  const placed = new Set<string>([
    ...plan.income.items.flatMap((i) => i.movements.map((m) => m.id)),
    ...line.candidates.map((c) => c.id),
  ])

  const assign = (choice: Choice) =>
    links.assign(choice.id, `Listo: ${line.name} entró con ese movimiento.`)

  return (
    <ResponsiveDialogContent
      {...returnFocus}
      title={line.name}
      description={`${money(line.amount)} · ${incomeStatus(line, plan)}`}
      footer={
        plan.editable && (
          <Button variant="secondary" size="lg" className="lg:h-11" onClick={onEdit}>
            Editar ingreso
          </Button>
        )
      }
    >
      {mark && !arrived && (
        <div className="flex flex-col items-start gap-2">
          <p className="text-body text-pretty text-muted-foreground">
            Cuenta una vez, como lo planeaste.
          </p>
          <Button
            variant="secondary"
            aria-disabled={busy || undefined}
            onClick={() =>
              links.unmarkPaid(mark, `Listo: ${line.name} vuelve a estar por llegar.`)
            }
          >
            Aún no me ha llegado
          </Button>
        </div>
      )}

      {line.status !== 'done' && (
        <section aria-labelledby={pickId} className="flex flex-col gap-2">
          <h3 id={pickId} className="text-body font-semibold">
            ¿Cuál fue?
          </h3>
          {line.candidates.length > 0 && (
            <ul className="-mx-3 flex flex-col">
              {line.candidates.map((c) => (
                <li key={c.id}>
                  <MovementChoice movement={c} busy={busy} onPick={assign} />
                </li>
              ))}
            </ul>
          )}
          <MonthMovements
            month={plan.month}
            side="in"
            planned={line.amount.cents}
            placed={placed}
            busy={busy}
            onPick={assign}
          />
          <Button
            variant="secondary"
            className="mt-2 self-start"
            aria-disabled={busy || undefined}
            onClick={() =>
              links.markPaid(`Listo: ${line.name} ya llegó en ${monthWord(plan.month)}.`)
            }
          >
            Me llegó a otra cuenta
          </Button>
        </section>
      )}

      <PaidMovements
        title="Entró con"
        movements={line.movements}
        busy={busy}
        onNotThis={(movement) =>
          links.notThis(movement, `Listo: ese movimiento no es ${line.name}.`)
        }
      />

      {mark && arrived && (
        <div className="flex flex-col items-start gap-1">
          <p className="text-small font-medium text-pretty text-muted-foreground">
            También dijiste que te llegó a otra cuenta.
          </p>
          <Button
            variant="ghost"
            className="-ml-3"
            aria-disabled={busy || undefined}
            onClick={() =>
              links.unmarkPaid(mark, 'Listo: ya no dice que te llegó a otra cuenta.')
            }
          >
            Quitar la marca
          </Button>
        </div>
      )}

      {failure && <FormFailure title="No pudimos guardarlo" message={failure} />}
    </ResponsiveDialogContent>
  )
}
