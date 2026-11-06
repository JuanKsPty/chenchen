/**
 * <HomeNotices accounts onReviewUploads className? />
 *
 *   accounts         the account list (useAccounts().data?.accounts)
 *   onReviewUploads  opens the review of the files Claude sent (HomePage owns the
 *                    dialog: importing the first one can swap Inicio's empty state
 *                    for the full page, and the dialog must outlive that)
 *
 * What needs a look, under Patrimonio (spec card/surface "Aviso No cuadra"):
 *   - one attention notice per account that does not match its bank:
 *     "<cuenta> no cuadra por $25.00" · "Desde el 03 oct." · [Revisar], which
 *     opens the account with its review sheet (accountHref(id, {review: true}));
 *   - a pending notice for accounts with no starting balance ("2 cuentas sin saldo
 *     inicial", to Cuentas, where each one shows its chip);
 *     (a card someone else pays is in neither: it has no balance of the owner's to
 *     match, and accountStatus says nothing of it)
 *   - a pending notice for transfers waiting for a yes or no ("1 transferencia por
 *     confirmar", to Gastos#transferencias);
 *   - a pending notice for purchases the owner noted that their statement does not
 *     confirm ("2 compras por revisar" · "Tu estado no las confirma todavía.", to
 *     Gastos#compras), counted from the accounts' `to_review`, which the list already
 *     has: no request of its own;
 *   - a pending notice for the statements Claude sent that did not enter on their own
 *     ("Revisa 1 importe que llegó desde Claude" · "No entró solo: míralo antes de
 *     importarlo."), whose "Revisar" calls onReviewUploads. It is the first of the
 *     pending ones: what it holds changes balances. Inicio shows it before the first
 *     import too (HomePage), since the first file of an account is imported there.
 * Renders nothing when there is nothing to say. Counts only, no amounts, on the
 * pending ones: Inicio shows at most five figures before its first list.
 */
import { ArrowLeftRightIcon, FileClockIcon, ReceiptIcon, ScaleIcon } from 'lucide-react'
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
import { ACCOUNTS_PATH, CAPTURES_HREF, TRANSFERS_HREF, accountHref } from '@/lib/nav'
import { useStatementUploads, useTransferSuggestions } from '@/lib/queries'
import { cn } from '@/lib/utils'

type Mismatch = { account: Account; status: Extract<AccountStatus, { kind: 'no-cuadra' }> }

export function HomeNotices({
  accounts,
  onReviewUploads,
  className,
}: {
  accounts: Account[]
  onReviewUploads: () => void
  className?: string
}) {
  // With no accounts (Inicio's empty state) there is nothing to transfer between.
  const suggestions = useTransferSuggestions({ enabled: accounts.length > 0 })
  const uploads = useStatementUploads()

  const mismatches: Mismatch[] = []
  let unanchored = 0
  let purchases = 0
  for (const account of accounts) {
    const status = accountStatus(account)
    if (status?.kind === 'no-cuadra') mismatches.push({ account, status })
    else if (status?.kind === 'sin-partida') unanchored += 1
    purchases += account.to_review ?? 0
  }
  const transfers = suggestions.data?.suggestions.length ?? 0
  // Empty when this deployment has no MCP: nothing can arrive from Claude. A failure
  // says nothing either; the rest of Inicio does not wait on it.
  const fromClaude = uploads.data?.uploads.length ?? 0

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

  if (fromClaude > 0) {
    items.push({
      key: 'desde-claude',
      notice: (
        <Notice
          tone="pending"
          icon={FileClockIcon}
          title={
            fromClaude === 1
              ? 'Revisa 1 importe que llegó desde Claude'
              : `Revisa ${fromClaude} importes que llegaron desde Claude`
          }
          description={
            fromClaude === 1
              ? 'No entró solo: míralo antes de importarlo.'
              : 'No entraron solos: míralos antes de importarlos.'
          }
          action={{
            label: 'Revisar',
            ariaLabel: 'Revisar importes de Claude',
            onClick: onReviewUploads,
          }}
        />
      ),
    })
  }

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

  if (purchases > 0) {
    items.push({
      key: 'compras',
      notice: (
        <Notice
          tone="pending"
          icon={ReceiptIcon}
          title={`${purchases} ${purchases === 1 ? 'compra' : 'compras'} por revisar`}
          description={
            purchases === 1
              ? 'Tu estado no la confirma todavía.'
              : 'Tu estado no las confirma todavía.'
          }
          action={{
            label: 'Revisar',
            ariaLabel: 'Revisar compras',
            to: CAPTURES_HREF,
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
