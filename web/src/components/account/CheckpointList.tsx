/**
 * <CheckpointList account />
 *
 * The review sheet's "Saldos del banco": every figure the bank stated for this
 * account, newest first, each with whether the movements reach it. A row's menu sets
 * or clears it as the starting balance, and deletes one the owner typed. "Anotar
 * saldo" opens the CheckpointDialog on top.
 */
import {
  EllipsisVerticalIcon,
  PinIcon,
  PinOffIcon,
  PlusIcon,
  Trash2Icon,
} from 'lucide-react'
import { useId } from 'react'
import { toast } from 'sonner'

import type { Account, Checkpoint } from '@/api/types'
import { CheckpointDialog } from '@/components/CheckpointDialog'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Skeleton } from '@/components/ui/skeleton'
import { formatCivilDate } from '@/lib/format'
import {
  useCheckpoints,
  useDeleteCheckpoint,
  usePinCheckpoint,
  useReconciliation,
} from '@/lib/queries'
import { LOAD_FAILED, bankFigures, differenceById, shortDate } from './bank'
import { BankFigureRow } from './BankFigureRow'

export function CheckpointList({ account }: { account: Account }) {
  const headingId = useId()
  const brokerage = account.type === 'brokerage'
  const checkpoints = useCheckpoints(account.id)
  const reconciliation = useReconciliation(account.id)
  const pin = usePinCheckpoint(account.id)
  const remove = useDeleteCheckpoint(account.id)

  const figures = bankFigures(checkpoints.data?.checkpoints)
  const differences = differenceById(reconciliation.data?.checks)
  const busy = pin.isPending || remove.isPending

  const setPinned = (checkpoint: Checkpoint | null) =>
    pin.mutate(checkpoint?.id ?? null, {
      onSuccess: () =>
        toast(
          checkpoint
            ? `Listo: tu saldo se calcula desde el ${shortDate(checkpoint.as_of)}.`
            : 'Quitaste el saldo inicial.',
        ),
      onError: () => toast(LOAD_FAILED),
    })

  const deleteFigure = (checkpoint: Checkpoint) =>
    remove.mutate(checkpoint.id, {
      onSuccess: () => toast('Borraste ese saldo.'),
      onError: () => toast(LOAD_FAILED),
    })

  return (
    <section aria-labelledby={headingId} className="flex flex-col gap-1">
      <div className="-mr-3 flex min-h-11 items-center justify-between gap-3">
        <h3 id={headingId} className="text-body font-semibold">
          {brokerage ? 'Saldos reportados' : 'Saldos del banco'}
        </h3>
        <CheckpointDialog
          account={account}
          anchor={!account.anchored}
          trigger={
            <Button variant="ghost">
              <PlusIcon aria-hidden="true" />
              Anotar saldo
            </Button>
          }
        />
      </div>

      {checkpoints.isLoading ? (
        <Skeleton className="h-32 w-full" />
      ) : checkpoints.isError ? (
        <p className="text-body text-pretty text-muted-foreground">{LOAD_FAILED}</p>
      ) : figures.length === 0 ? (
        <p className="text-body text-pretty text-muted-foreground">
          {brokerage
            ? 'Todavía no hay saldos. Sincroniza o anota el efectivo que te muestra Interactive Brokers.'
            : 'Todavía no hay saldos del banco. Importa un estado de cuenta o anota el saldo que te da tu banco.'}
        </p>
      ) : (
        <ul className="-mx-4 flex flex-col">
          {figures.map((checkpoint) => (
            <li key={checkpoint.id}>
              <BankFigureRow
                account={account}
                checkpoint={checkpoint}
                difference={differences.get(checkpoint.id)}
                actions={
                  <DropdownMenu>
                    <DropdownMenuTrigger asChild>
                      <Button
                        variant="plain"
                        size="icon"
                        disabled={busy}
                        aria-label={`Opciones del saldo del ${formatCivilDate(checkpoint.as_of)}`}
                      >
                        <EllipsisVerticalIcon aria-hidden="true" />
                      </Button>
                    </DropdownMenuTrigger>
                    <DropdownMenuContent align="end" className="w-auto">
                      {checkpoint.pinned ? (
                        <DropdownMenuItem onSelect={() => setPinned(null)}>
                          <PinOffIcon aria-hidden="true" />
                          Quitar como saldo inicial
                        </DropdownMenuItem>
                      ) : (
                        <DropdownMenuItem onSelect={() => setPinned(checkpoint)}>
                          <PinIcon aria-hidden="true" />
                          Usar como saldo inicial
                        </DropdownMenuItem>
                      )}
                      {checkpoint.source === 'manual' && (
                        <DropdownMenuItem
                          variant="destructive"
                          onSelect={() => deleteFigure(checkpoint)}
                        >
                          <Trash2Icon aria-hidden="true" />
                          Borrar este saldo
                        </DropdownMenuItem>
                      )}
                    </DropdownMenuContent>
                  </DropdownMenu>
                }
              />
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}
