/**
 * <DeleteAccountDialog account open onOpenChange />
 *
 * The inverse confirmation (spec voice.deleteConfirm): "¿Eliminar <cuenta>? Se borran
 * sus 38 movimientos de chenchén; tu cuenta en el banco no cambia." with the dark
 * "Eliminar cuenta" button beside "Cancelar". Never orange. On success it says so
 * in a toast and goes back to Cuentas (lib/queries' useDeleteAccount drops the
 * deleted account from the cache first, so the page never refetches it).
 */
import { CircleAlertIcon } from 'lucide-react'
import { useNavigate } from 'react-router-dom'
import { toast } from 'sonner'

import type { Account } from '@/api/types'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Spinner } from '@/components/ui/spinner'
import { shortAccountLabel } from '@/lib/account'
import { useReturnFocus } from '@/lib/focus'
import { ACCOUNTS_PATH } from '@/lib/nav'
import { useDeleteAccount } from '@/lib/queries'

export function DeleteAccountDialog({
  account,
  open,
  onOpenChange,
}: {
  account: Account
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const navigate = useNavigate()
  const returnFocus = useReturnFocus()
  const name = shortAccountLabel(account)
  const remove = useDeleteAccount()
  const movements =
    account.movements === 1 ? 'su único movimiento' : `sus ${account.movements} movimientos`
  const brokerage = account.type === 'brokerage'

  const confirm = () =>
    remove.mutate(account.id, {
      onSuccess: () => {
        toast(`Eliminaste ${name}.`)
        navigate(ACCOUNTS_PATH, { replace: true })
      },
    })

  // Every way out (Escape, the overlay, Cancelar) clears a failed attempt, so the
  // next opening does not show its error before anything was tried.
  const changeOpen = (next: boolean) => {
    onOpenChange(next)
    if (!next) remove.reset()
  }

  return (
    <Dialog open={open} onOpenChange={changeOpen}>
      <DialogContent className="sm:max-w-104" {...returnFocus}>
        <DialogHeader>
          <DialogTitle>¿Eliminar {name}?</DialogTitle>
          <DialogDescription>
            {account.movements === 0
              ? brokerage
                ? 'Se borra su conexión con Interactive Brokers; tu cuenta allá no cambia.'
                : 'Tu cuenta en el banco no cambia.'
              : brokerage
                ? `Se borran ${movements} de chenchén y su conexión con Interactive Brokers; tu cuenta allá no cambia.`
                : `Se borran ${movements} de chenchén; tu cuenta en el banco no cambia.`}
          </DialogDescription>
        </DialogHeader>

        {remove.isError && (
          <Alert variant="attention">
            <CircleAlertIcon aria-hidden="true" />
            <AlertDescription className="text-foreground">
              Algo falló de nuestro lado. Tus datos están bien; intenta otra vez en un
              momento.
            </AlertDescription>
          </Alert>
        )}

        <DialogFooter>
          <Button type="button" variant="secondary" onClick={() => changeOpen(false)}>
            Cancelar
          </Button>
          <Button
            type="button"
            variant="destructive"
            disabled={remove.isPending}
            onClick={confirm}
          >
            {remove.isPending && <Spinner />}
            Eliminar cuenta
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
