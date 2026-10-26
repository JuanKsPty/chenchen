/**
 * <HomeNotices accounts className? />
 *
 *   accounts  the account list (useAccounts().data?.accounts)
 *
 * What needs a look, under Patrimonio (spec card/surface "Aviso No cuadra"):
 *   - one attention notice per account that does not match its bank:
 *     "<cuenta> no cuadra por $25.00" · "Desde el 03 oct." · [Revisar], which
 *     opens the account with its review sheet (accountHref(id, {review: true}));
 *   - a pending notice for accounts with no starting balance ("2 cuentas sin saldo
 *     inicial", to Cuentas, where each one shows its chip);
 *   - a pending notice for transfers waiting for a yes or no ("1 transferencia por
 *     confirmar", to Gastos#transferencias).
 * Renders nothing when there is nothing to say. Counts only, no amounts, on the
 * pending ones: Inicio shows at most five figures before its first list.
 */
import { ArrowLeftRightIcon, ScaleIcon } from 'lucide-react'
import type { ReactNode } from 'react'

import type { Account } from '@/api/types'
import { Amount, Notice } from '@/components/kit'
import {
  accountRowTitle,
  accountStatus,
  shortAccountLabel,
  type AccountStatus,
} from '@/lib/account'
import { formatCivilDayMonth } from '@/lib/format'
import { ACCOUNTS_PATH, TRANSFERS_HREF, accountHref } from '@/lib/nav'
import { useTransferSuggestions } from '@/lib/queries'
import { cn } from '@/lib/utils'

type Mismatch = { account: Account; status: Extract<AccountStatus, { kind: 'no-cuadra' }> }

export function HomeNotices({
  accounts,
  className,
}: {
  accounts: Account[]
  className?: string
}) {
  const suggestions = useTransferSuggestions()

  const mismatches: Mismatch[] = []
  let unanchored = 0
  for (const account of accounts) {
    const status = accountStatus(account)
    if (status?.kind === 'no-cuadra') mismatches.push({ account, status })
    else if (status?.kind === 'sin-partida') unanchored += 1
  }
  const transfers = suggestions.data?.suggestions.length ?? 0

  const items: { key: string; notice: ReactNode }[] = [
    ...mismatches.map(({ account, status }) => ({
      key: account.id,
      notice: (
        <Notice
          title={
            <>
              {shortAccountLabel(account)} no cuadra por{' '}
              <Amount amount={status.difference} size="inline" />
            </>
          }
          description={`Desde el ${formatCivilDayMonth(status.asOf)}.`}
          action={{
            label: 'Revisar',
            ariaLabel: `Revisar ${accountRowTitle(account)}`,
            to: accountHref(account.id, { review: true }),
          }}
        />
      ),
    })),
  ]

  if (unanchored > 0) {
    items.push({
      key: 'sin-partida',
      notice: (
        <Notice
          tone="pending"
          icon={ScaleIcon}
          title={`${unanchored} ${unanchored === 1 ? 'cuenta' : 'cuentas'} sin saldo inicial`}
          description="Anota el saldo de tu estado para que cuadre."
          action={{
            label: 'Revisar',
            ariaLabel: 'Revisar cuentas sin saldo inicial',
            to: ACCOUNTS_PATH,
          }}
        />
      ),
    })
  }

  if (transfers > 0) {
    items.push({
      key: 'transferencias',
      notice: (
        <Notice
          tone="pending"
          icon={ArrowLeftRightIcon}
          title={`${transfers} ${transfers === 1 ? 'transferencia' : 'transferencias'} por confirmar`}
          description="Un pago entre tus cuentas no es un gasto."
          action={{
            label: 'Revisar',
            ariaLabel: 'Revisar transferencias',
            to: TRANSFERS_HREF,
          }}
        />
      ),
    })
  }

  if (items.length === 0) return null

  return (
    // Card spacing (spec densityNotes): 16 apart on mobile, 24 from lg.
    <ul aria-label="Avisos" className={cn('flex flex-col gap-4 lg:gap-6', className)}>
      {items.map((item) => (
        <li key={item.key}>{item.notice}</li>
      ))}
    </ul>
  )
}
