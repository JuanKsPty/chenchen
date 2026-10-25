/**
 * <AccountRow account to? className? />
 *
 *   account  the Account to show
 *   to       where the row goes; defaults to accountHref(account.id)
 *
 * Spec accountRow: a link, grid [44 | 1fr | auto], gap 12, padding 12 16, hover and
 * press surface2 as a pill.
 *   - Tile with the institution's initials.
 *   - Title (body 600): the alias, else "Ahorros" / "Corriente" / "Tarjeta de
 *     crédito" / "Interactive Brokers". Subtitle (small muted): the bank's name for
 *     it and the masked number, "<banco> Corriente ···0932", "<institución> ···4417",
 *     "Inversiones · U···567". The title truncates; in the subtitle only the name
 *     does, so the digits that tell two accounts apart always show (320 and up).
 *
 * <AccountRowSubtitle account className? /> is that second line on its own, for a
 * row built elsewhere (Ajustes › Conexiones).
 *   - Right: the balance (body 600, tabular). A card reads "Debes $486.20" in the
 *     text colour, never red. A balance with no starting point is muted.
 *   - Under the balance from md, or as a third line of the text column on mobile:
 *     the StatusChip; for a card with `account.statement`, "Pago 05 nov · Mínimo
 *     $25.00" in its place (a card that does not match still shows the chip).
 * Desktop rows are at least 72px; mobile rows grow to about 100 with the chip.
 *
 * Rows go in an <AccountGroup> (or any surface with 4px padding and 4px gaps).
 */
import type { ReactNode } from 'react'
import { Link } from 'react-router-dom'

import type { Account, CardStatement } from '@/api/types'
import {
  accountRowSubtitleParts,
  accountRowTitle,
  accountStatus,
  headline,
  type AccountStatus,
} from '@/lib/account'
import { formatCivilDayMonth } from '@/lib/format'
import { accountHref } from '@/lib/nav'
import { cn } from '@/lib/utils'
import { Amount } from './Amount'
import { StatusChip } from './StatusChip'
import { Tile } from './Tile'

export function AccountRow({
  account,
  to,
  className,
}: {
  account: Account
  to?: string
  className?: string
}) {
  const status = accountStatus(account)
  const { amount } = headline(account)
  const unanchored = !account.anchored && account.movements > 0
  const meta = <RowMeta account={account} status={status} />
  const hasMeta = status !== null || Boolean(account.statement)

  return (
    <Link
      to={to ?? accountHref(account.id)}
      className={cn(
        'grid grid-cols-[2.75rem_minmax(0,1fr)_auto] items-start gap-3 rounded-full px-4 py-3 text-foreground transition-colors duration-180 ease-out hover:bg-muted active:bg-muted md:min-h-18 md:items-center',
        className,
      )}
    >
      <Tile institution={account.institution} />
      <span className="flex min-w-0 flex-col">
        <span className="truncate text-body font-semibold">{accountRowTitle(account)}</span>
        <AccountRowSubtitle account={account} />
        {hasMeta && <span className="mt-1 flex md:hidden">{meta}</span>}
      </span>
      <span className="flex flex-col items-end gap-1">
        {account.class === 'liability' ? (
          <span className="text-body font-semibold whitespace-nowrap">
            Debes <Amount amount={amount} size="inline" />
          </span>
        ) : (
          <Amount amount={amount} tone={unanchored ? 'muted' : 'auto'} />
        )}
        {hasMeta && <span className="hidden md:flex">{meta}</span>}
      </span>
    </Link>
  )
}

export function AccountRowSubtitle({
  account,
  className,
}: {
  account: Account
  className?: string
}) {
  const { name, number } = accountRowSubtitleParts(account)
  return (
    <span
      className={cn('flex min-w-0 text-small font-medium text-muted-foreground', className)}
    >
      <span className="truncate">{name}</span>
      {/* The space lives in the digits' span, so a screen reader still hears two
          words when the name is cut. */}
      <span className="shrink-0 whitespace-pre">{` ${number}`}</span>
    </span>
  )
}

function RowMeta({ account, status }: { account: Account; status: AccountStatus | null }) {
  if (account.statement && status?.kind !== 'no-cuadra') {
    return <CardDue statement={account.statement} />
  }
  return status ? <StatusChip status={status} /> : null
}

/** "Pago 05 nov · Mínimo $25.00", or "Corte 15 oct" when the due date is unknown. */
function CardDue({ statement }: { statement: CardStatement }) {
  const parts: ReactNode[] = []
  if (statement.due_on) {
    parts.push(<span key="due">Pago {formatCivilDayMonth(statement.due_on)}</span>)
  }
  if (statement.minimum_payment) {
    parts.push(
      <span key="min">
        Mínimo <Amount amount={statement.minimum_payment} size="inline" />
      </span>,
    )
  }
  if (parts.length === 0) {
    parts.push(<span key="cut">Corte {formatCivilDayMonth(statement.closing_on)}</span>)
  }
  return (
    <span className="text-small font-medium whitespace-nowrap text-muted-foreground">
      {parts.flatMap((part, index) => (index === 0 ? [part] : [' · ', part]))}
    </span>
  )
}
