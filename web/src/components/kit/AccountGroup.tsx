/**
 * <AccountGroup title total? accounts? headingLevel? className? children? />
 *
 *   title         "Cuentas", "Tarjetas" or "Inversiones"
 *   total         the group's total, right of the title (600, tabular, text colour)
 *   accounts      rendered as <AccountRow>s, in the order given
 *   children      extra rows after the accounts, e.g. a ghost "Conectar Interactive
 *                 Brokers" button in Inversiones
 *   headingLevel  2 (default) or 3
 *
 * With lib/account `groupAccounts(accounts)`:
 *   groups.map((g) => <AccountGroup key={g.key} title={g.title} total={g.total}
 *                                    accounts={g.accounts} />)
 *
 * Spec accountRow: rows live in a surface card with 4px padding and 4px between
 * them, no lines. The group heading is small 14/20 600 muted, padded 16 16 4.
 */
import { useId, type ReactNode } from 'react'

import type { Account, Amount as AmountValue } from '@/api/types'
import { cn } from '@/lib/utils'
import { AccountRow } from './AccountRow'
import { Amount } from './Amount'

export function AccountGroup({
  title,
  total,
  accounts = [],
  headingLevel = 2,
  className,
  children,
}: {
  title: ReactNode
  total?: AmountValue
  accounts?: Account[]
  headingLevel?: 2 | 3
  className?: string
  children?: ReactNode
}) {
  const headingId = useId()
  const Heading = headingLevel === 3 ? 'h3' : 'h2'

  return (
    <section
      aria-labelledby={headingId}
      className={cn('flex flex-col gap-1 rounded-2xl bg-card p-1 shadow-card', className)}
    >
      <div className="flex items-baseline justify-between gap-3 px-4 pt-3 pb-1">
        <Heading id={headingId} className="text-small font-semibold text-muted-foreground">
          {title}
        </Heading>
        {total && (
          <Amount
            amount={total}
            size="inline"
            className="text-small font-semibold text-foreground"
          />
        )}
      </div>
      {accounts.map((account) => (
        <AccountRow key={account.id} account={account} />
      ))}
      {children}
    </section>
  )
}
