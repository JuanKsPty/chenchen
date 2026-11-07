/**
 * <FixedPaymentSheet plan line open onOpenChange onEdit />
 *
 *   line    the fixed payment's month, as the plan read it
 *   onEdit  "Editar pago fijo" (only when the month can still be edited): the sheet
 *           closes and the payment's dialog opens
 *
 * What a tapped fixed payment row opens: which movement paid it, said by the owner.
 * The plan recognises a payment by its text and amount; this is for when it could
 * not, which otherwise leaves the payment's money aside and its movement in what
 * went out of what is free: counted twice, until the owner says which it was.
 *
 * While it is still to be paid:
 *   - A movement with its text that went out as a transfer between the owner's
 *     accounts (a rent paid by bank transfer): "No es transferencia" files it as
 *     spending with the same change Categorizar makes, and the plan then sees it.
 *   - "¿Cuál fue?": the movements that come close (candidates), then the rest of
 *     the month's spending, closest amount first. Picking one says it paid this
 *     month of the payment.
 *   - "Lo pagué desde otra cuenta": paid from somewhere chenchén does not see; it
 *     counts once, as planned.
 * Once it went out, the movements that paid it, each with "No es este", which says
 * that movement did not pay it, whoever had picked it (usePlanLinks). Marked paid
 * from another account, "Aún no lo he pagado" takes that back by its link. A
 * statement imported after the mark may bring the movement that paid it: what went
 * out then counts, the mark changes nothing while it does, and "Quitar la marca"
 * takes it back. So what was said can be undone after its toast is gone, or another
 * day.
 *
 * A past month can still be linked (it is how the month gets right) but not edited;
 * next month opens its dialog instead (PlanPage), since nothing has gone out yet.
 * Each answer closes the sheet with a toast whose "Deshacer" takes it back; a
 * refusal stays in the sheet as an Alert.
 */
import { useId, useState } from 'react'
import { toast } from 'sonner'

import { updateEntry } from '@/api/endpoints'
import type { MonthPlan, PlanFixedLine } from '@/api/types'
import { ResponsiveDialogContent } from '@/components/accounts/ResponsiveDialog'
import { problemMessage } from '@/components/settings/problem'
import { Button } from '@/components/ui/button'
import { Dialog } from '@/components/ui/dialog'
import { Spinner } from '@/components/ui/spinner'
import { useReturnFocus } from '@/lib/focus'
import { money } from '@/lib/format'
import { useMoneyInvalidation, useUpdateEntry } from '@/lib/queries'
import { FormFailure, useOpenRound } from './forms'
import {
  MonthMovements,
  MovementChoice,
  MovementLine,
  PaidMovements,
  type Choice,
} from './MovementPicker'
import { usePlanLinks } from './usePlan'
import { fixedStatus, monthWord } from './words'

export function FixedPaymentSheet({
  plan,
  line,
  open,
  onOpenChange,
  onEdit,
}: {
  plan: MonthPlan
  line: PlanFixedLine | null
  open: boolean
  onOpenChange: (open: boolean) => void
  onEdit: (line: PlanFixedLine) => void
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

function SheetBody({
  plan,
  line,
  returnFocus,
  onDone,
  onEdit,
}: {
  plan: MonthPlan
  line: PlanFixedLine
  returnFocus: ReturnType<typeof useReturnFocus>
  onDone: () => void
  onEdit: () => void
}) {
  const [failure, setFailure] = useState<string | null>(null)
  const links = usePlanLinks({ item_id: line.id, month: plan.month }, onDone, setFailure)
  const update = useUpdateEntry()
  const invalidateMoney = useMoneyInvalidation()
  const pickId = useId()

  // A month marked paid is out too, so only one still to be paid asks which it was.
  const toPay = line.status !== 'out'
  const hint = line.hint
  const busy = links.busy || update.isPending
  // The link by which the owner said it was paid from another account, if they did.
  const mark = line.marked_paid ? line.paid_link_id : null

  // Every movement that already has a place: the plan's payments, and the ones
  // offered above the month's list.
  const placed = new Set<string>([
    ...plan.fixed.flatMap((f) => f.movements.map((m) => m.id)),
    ...line.candidates.map((c) => c.id),
    ...(hint ? [hint.movement.id] : []),
  ])

  const notTransfer = (movement: string) => {
    if (busy) return
    setFailure(null)
    update.mutate(
      { id: movement, patch: { transfer: false } },
      {
        onSuccess: () => {
          toast('Listo: ya cuenta como gasto.', {
            action: {
              label: 'Deshacer',
              // The sheet is gone by now, so this goes straight to the server.
              onClick: () => {
                updateEntry(movement, { transfer: true }).then(
                  () => invalidateMoney(),
                  () => toast('No se pudo deshacer. Intenta otra vez.'),
                )
              },
            },
          })
          onDone()
        },
        onError: (error) => setFailure(problemMessage(error)),
      },
    )
  }

  const assign = (choice: Choice) =>
    links.assign(choice.id, `Listo: ${line.name} salió con ese movimiento.`)

  return (
    <ResponsiveDialogContent
      {...returnFocus}
      title={line.name}
      description={`${money(line.amount)} al mes · ${fixedStatus(line, plan, { short: true })}`}
      footer={
        plan.editable && (
          <Button variant="secondary" size="lg" className="lg:h-11" onClick={onEdit}>
            Editar pago fijo
          </Button>
        )
      }
    >
      {mark && line.paid === null && (
        <div className="flex flex-col items-start gap-2">
          <p className="text-body text-pretty text-muted-foreground">
            Cuenta una vez, como lo planeaste.
          </p>
          <Button
            variant="secondary"
            aria-disabled={busy || undefined}
            onClick={() =>
              links.unmarkPaid(mark, `Listo: ${line.name} vuelve a estar por pagar.`)
            }
          >
            Aún no lo he pagado
          </Button>
        </div>
      )}

      {hint && (
        <section className="flex flex-col gap-3 rounded-2xl bg-muted p-4">
          <div className="flex flex-col gap-1">
            <h3 className="text-body font-semibold">¿Lo pagaste con esta transferencia?</h3>
            <p className="text-small font-medium text-pretty text-muted-foreground">
              Hoy cuenta como transferencia entre tus cuentas, no como gasto.
            </p>
          </div>
          <MovementLine movement={hint.movement} />
          <Button
            variant="secondary"
            className="self-start bg-card hover:bg-line"
            aria-disabled={busy || undefined}
            onClick={() => notTransfer(hint.movement.id)}
          >
            {update.isPending && <Spinner aria-hidden="true" />}
            No es transferencia
          </Button>
        </section>
      )}

      {toPay && (
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
            side="out"
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
              links.markPaid(
                `Listo: ${line.name} ya está pagado en ${monthWord(plan.month)}.`,
              )
            }
          >
            Lo pagué desde otra cuenta
          </Button>
        </section>
      )}

      <PaidMovements
        title="Salió con"
        movements={line.movements}
        busy={busy}
        onNotThis={(movement) =>
          links.notThis(movement, `Listo: ese movimiento no es ${line.name}.`)
        }
      />

      {mark && line.paid !== null && (
        <div className="flex flex-col items-start gap-1">
          <p className="text-small font-medium text-pretty text-muted-foreground">
            También dijiste que lo pagaste desde otra cuenta. Cuenta lo que salió.
          </p>
          <Button
            variant="ghost"
            className="-ml-3"
            aria-disabled={busy || undefined}
            onClick={() =>
              links.unmarkPaid(mark, 'Listo: ya no dice que lo pagaste desde otra cuenta.')
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
