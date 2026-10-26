/**
 * <BankFiguresCard account onReview className? />
 *
 * Desktop only, beside the movements (the 5 columns the artboard gives the side card):
 * the latest four figures the bank stated and whether the movements reach each one.
 * "Ver detalle" opens the review sheet, where they can be changed. Below lg the page
 * keeps to the spec's single column (balance, notice, movements), so the page does
 * not render it, and the review sheet is reached from the notice or the chip.
 */
import type { Account } from '@/api/types'
import { SectionCard } from '@/components/kit'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { cn } from '@/lib/utils'
import { useCheckpoints, useReconciliation } from '@/lib/queries'
import { LOAD_FAILED, bankFigures, differenceById } from './bank'
import { BankFigureRow } from './BankFigureRow'

const SHOWN = 4

export function BankFiguresCard({
  account,
  onReview,
  className,
}: {
  account: Account
  onReview: () => void
  className?: string
}) {
  const checkpoints = useCheckpoints(account.id)
  const reconciliation = useReconciliation(account.id)
  const figures = bankFigures(checkpoints.data?.checkpoints)
  const differences = differenceById(reconciliation.data?.checks)

  return (
    <SectionCard
      title="Saldos del banco"
      layout="list"
      className={className}
      action={
        <Button variant="ghost" onClick={onReview}>
          Ver detalle
        </Button>
      }
    >
      {checkpoints.isLoading ? (
        <FigureRowsSkeleton />
      ) : checkpoints.isError ? (
        <p className="px-4 pb-3 text-body text-pretty text-muted-foreground">
          {LOAD_FAILED}
        </p>
      ) : figures.length === 0 ? (
        <p className="px-4 pb-3 text-body text-pretty text-muted-foreground">
          Todavía no hay saldos del banco. Importa un estado de cuenta o anota el saldo que
          te da tu banco.
        </p>
      ) : (
        <ul className="flex flex-col">
          {figures.slice(0, SHOWN).map((checkpoint) => (
            <li key={checkpoint.id}>
              <BankFigureRow
                account={account}
                checkpoint={checkpoint}
                difference={differences.get(checkpoint.id)}
              />
            </li>
          ))}
        </ul>
      )}
    </SectionCard>
  )
}

/**
 * AccountPage's stand-in for this card while the account itself loads (its type is
 * not known yet): the 44px header row with a placeholder title, then the rows.
 */
export function BankFiguresSkeleton({ className }: { className?: string }) {
  return (
    <div
      aria-hidden="true"
      className={cn(
        'flex min-w-0 flex-col rounded-2xl bg-card p-1 shadow-card md:p-2',
        className,
      )}
    >
      <div className="box-content flex min-h-11 items-center pt-3 pb-1 pl-3 md:pt-4 md:pb-2 md:pl-4">
        <Skeleton className="h-6 w-40 rounded-full" />
      </div>
      <FigureRowsSkeleton />
    </div>
  )
}

/** Four rows of BankFigureRow's shape, so nothing moves when the figures arrive. */
function FigureRowsSkeleton() {
  return (
    <ul aria-hidden="true" className="flex flex-col">
      {Array.from({ length: SHOWN }, (_, row) => (
        <li
          key={row}
          className="grid min-h-16 grid-cols-[2.5rem_minmax(0,1fr)_auto] items-center gap-3 px-4 py-2.5 md:min-h-14 md:py-2"
        >
          <Skeleton className="size-10 rounded-full" />
          <span className="flex flex-col gap-2">
            <Skeleton className="h-4 w-16 rounded-full" />
            <Skeleton className="h-3 w-24 rounded-full" />
          </span>
          <Skeleton className="h-4 w-20 rounded-full" />
        </li>
      ))}
    </ul>
  )
}
