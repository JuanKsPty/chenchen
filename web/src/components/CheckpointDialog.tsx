/**
 * <CheckpointDialog account trigger? anchor? open? onOpenChange? />
 *
 * "Saldo según tu banco": the balance the bank showed at the end of a day, or for a
 * card what the statement says is owed at the cut-off, with the payment day and the
 * minimum. With "Usar como saldo inicial" it becomes the starting point the balance
 * is computed from; otherwise it is only compared, and a difference is shown rather
 * than corrected away.
 *
 *   trigger       an element that opens it (wrapped in a DialogTrigger). Leave it out
 *                 and pass `open` / `onOpenChange` to open it from code, e.g. from an
 *                 overflow menu item.
 *   anchor        start with "Usar como saldo inicial" ticked.
 *
 * A card's debt is typed positive, as the statement prints it, and stored negative.
 */
import { useState } from 'react'
import { CircleAlertIcon } from 'lucide-react'
import { toast } from 'sonner'

import { ApiError } from '@/api/client'
import type { Account } from '@/api/types'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '@/components/ui/dialog'
import { Field, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Spinner } from '@/components/ui/spinner'
import { useReturnFocus } from '@/lib/focus'
import { todayCivil } from '@/lib/format'
import { useAddCheckpoint } from '@/lib/queries'

export function CheckpointDialog({
  account,
  trigger,
  anchor = false,
  open: openProp,
  onOpenChange,
}: {
  account: Account
  trigger?: React.ReactNode
  /** Open with "Usar como saldo inicial" already ticked. */
  anchor?: boolean
  open?: boolean
  onOpenChange?: (open: boolean) => void
}) {
  const [openState, setOpenState] = useState(false)
  const open = openProp ?? openState
  const returnFocus = useReturnFocus()

  const setOpen = (next: boolean) => {
    if (openProp === undefined) setOpenState(next)
    onOpenChange?.(next)
  }

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      {trigger && <DialogTrigger asChild>{trigger}</DialogTrigger>}
      <DialogContent className="sm:max-w-120" {...returnFocus}>
        <CheckpointForm account={account} anchor={anchor} onDone={() => setOpen(false)} />
      </DialogContent>
    </Dialog>
  )
}

function CheckpointForm({
  account,
  anchor,
  onDone,
}: {
  account: Account
  anchor: boolean
  onDone: () => void
}) {
  const card = account.class === 'liability'
  const brokerage = account.type === 'brokerage'
  const [asOf, setAsOf] = useState(todayCivil())
  const [amount, setAmount] = useState('')
  const [inFavour, setInFavour] = useState(false)
  const [pin, setPin] = useState(anchor)
  const [dueOn, setDueOn] = useState('')
  const [minimum, setMinimum] = useState('')
  const add = useAddCheckpoint(account.id)

  const submit = (event: React.FormEvent) => {
    event.preventDefault()
    const typed = amount.replace(/[$,\s]/g, '')
    // What a card owes is a debt: negative from the owner's side.
    const balance = card && !inFavour && !typed.startsWith('-') ? `-${typed}` : typed
    add.mutate(
      {
        as_of: asOf,
        balance,
        pin,
        due_on: card && dueOn ? dueOn : undefined,
        minimum_payment: card && minimum ? minimum.replace(/[$,\s]/g, '') : undefined,
      },
      {
        onSuccess: () => {
          toast('Saldo guardado.')
          onDone()
        },
      },
    )
  }

  return (
    <form onSubmit={submit} className="flex flex-col gap-4">
      <DialogHeader>
        <DialogTitle>
          {brokerage ? 'Efectivo según Interactive Brokers' : 'Saldo según tu banco'}
        </DialogTitle>
        <DialogDescription>
          {card
            ? 'Cópialo de tu estado de cuenta: el corte, lo que debes, el pago y el mínimo.'
            : brokerage
              ? 'El efectivo que te muestra Interactive Brokers al final de un día. Lo comparamos con tus movimientos.'
              : 'El saldo que te muestra tu banco al final de un día. Lo comparamos con tus movimientos.'}
        </DialogDescription>
      </DialogHeader>

      <div className="grid gap-4 sm:grid-cols-2">
        <Field>
          <FieldLabel htmlFor="cp-date">{card ? 'Fecha de corte' : 'Fecha'}</FieldLabel>
          <Input
            id="cp-date"
            type="date"
            value={asOf}
            max={todayCivil()}
            onChange={(e) => setAsOf(e.target.value)}
            required
          />
        </Field>
        <Field>
          <FieldLabel htmlFor="cp-amount">
            {card ? 'Debes' : brokerage ? 'Efectivo' : 'Saldo'}
          </FieldLabel>
          <Input
            id="cp-amount"
            inputMode="decimal"
            autoComplete="off"
            placeholder="0.00"
            value={amount}
            onChange={(e) => setAmount(e.target.value)}
            required
          />
        </Field>
      </div>

      {card && (
        <>
          <label className="-my-1 flex min-h-11 cursor-pointer items-center gap-3 text-body">
            <Checkbox
              checked={inFavour}
              onCheckedChange={(checked) => setInFavour(checked === true)}
            />
            Es un saldo a tu favor
          </label>
          <div className="grid gap-4 sm:grid-cols-2">
            <Field>
              <FieldLabel htmlFor="cp-due">Fecha de pago</FieldLabel>
              <Input
                id="cp-due"
                type="date"
                value={dueOn}
                onChange={(e) => setDueOn(e.target.value)}
              />
            </Field>
            <Field>
              <FieldLabel htmlFor="cp-min">Pago mínimo</FieldLabel>
              <Input
                id="cp-min"
                inputMode="decimal"
                autoComplete="off"
                placeholder="0.00"
                value={minimum}
                onChange={(e) => setMinimum(e.target.value)}
              />
            </Field>
          </div>
        </>
      )}

      <label className="flex min-h-11 cursor-pointer items-start gap-3 py-1">
        <Checkbox
          className="mt-0.5"
          checked={pin}
          onCheckedChange={(checked) => setPin(checked === true)}
        />
        <span className="flex flex-col">
          <span className="text-body">Usar como saldo inicial</span>
          <span className="text-small font-medium text-muted-foreground">
            Tu saldo se calcula desde aquí. Si no, solo lo comparamos.
          </span>
        </span>
      </label>

      {add.isError && (
        <Alert variant="attention">
          <CircleAlertIcon aria-hidden="true" />
          <AlertDescription className="text-foreground">
            {add.error instanceof ApiError && add.error.status < 500
              ? add.error.message
              : 'Algo falló de nuestro lado. Tus datos están bien; intenta otra vez en un momento.'}
          </AlertDescription>
        </Alert>
      )}

      <DialogFooter>
        <Button type="button" variant="secondary" onClick={onDone}>
          Cancelar
        </Button>
        <Button type="submit" disabled={add.isPending || !amount.trim()}>
          {add.isPending && <Spinner />}
          Guardar
        </Button>
      </DialogFooter>
    </form>
  )
}
