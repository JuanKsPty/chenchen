/**
 * <BankFigureRow account checkpoint difference? actions? />
 *
 * One figure the bank stated: its day and where it came from on the left, the figure
 * and whether the movements reach it on the right ("Cuadra", or "No cuadra · $25.00"
 * in attention). The starting balance says so instead, since it matches by
 * definition. `actions` is a trailing 44px control (the review sheet's per-row menu).
 *
 * Laid out like a movement row: grid [40 | 1fr | auto], at least 64px on mobile and
 * 56 from md, padding 10 16.
 */
import type { ReactNode } from 'react'

import type { Account, Amount as AmountValue, Checkpoint } from '@/api/types'
import { Amount, Tile } from '@/components/kit'
import { cn } from '@/lib/utils'
import { SOURCE_ICON, SOURCE_LABEL, shortDate, statedAmount } from './bank'

export function BankFigureRow({
  account,
  checkpoint,
  difference,
  actions,
}: {
  account: Account
  checkpoint: Checkpoint
  difference?: AmountValue
  actions?: ReactNode
}) {
  return (
    <div
      className={cn(
        'grid min-h-16 grid-cols-[2.5rem_minmax(0,1fr)_auto] items-center gap-3 py-2.5 pl-4 md:min-h-14 md:py-2',
        actions ? 'grid-cols-[2.5rem_minmax(0,1fr)_auto_2.75rem] pr-1' : 'pr-4',
      )}
    >
      <Tile icon={SOURCE_ICON[checkpoint.source]} />
      <span className="flex min-w-0 flex-col">
        <span className="truncate text-body font-medium">
          {shortDate(checkpoint.as_of)}
        </span>
        <span className="truncate text-small font-medium text-muted-foreground">
          {checkpoint.pinned ? 'Saldo inicial' : SOURCE_LABEL[checkpoint.source]}
        </span>
      </span>
      <span className="flex flex-col items-end">
        <Amount amount={statedAmount(account, checkpoint.balance)} />
        <Result pinned={checkpoint.pinned} difference={difference} />
      </span>
      {actions}
    </div>
  )
}

function Result({ pinned, difference }: { pinned: boolean; difference?: AmountValue }) {
  if (pinned || !difference) return null
  if (difference.cents === 0) {
    return <span className="text-small font-semibold text-positive">Cuadra</span>
  }
  return (
    <span className="text-small font-semibold whitespace-nowrap text-attention">
      No cuadra · <Amount amount={Math.abs(difference.cents)} size="inline" />
    </span>
  )
}
