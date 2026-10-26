/**
 * <ReviewSheet account open onOpenChange />
 *
 * "Revisar con el banco" ("Revisar saldos" for a broker), opened by the No cuadra
 * notice's "Revisar", the page's "Ver saldos del banco" / "Ver detalle", and by
 * `?revisar=1`. A bottom sheet below lg, a dialog from lg: the decision CSS cannot
 * make, so it reads DESKTOP_QUERY.
 *
 * Inside, top to bottom: what is off and what to do about it, the bank's figures
 * (CheckpointList: set or clear the starting balance, delete one typed by hand, add
 * one), then each statement and the lines (ReconciliationPanel).
 */
import { CircleCheckIcon, FileUpIcon, ScaleIcon } from 'lucide-react'
import { useRef } from 'react'

import type { Account } from '@/api/types'
import { ReconciliationPanel } from '@/components/ReconciliationPanel'
import { useImport } from '@/components/import/ImportProvider'
import { Amount, Notice } from '@/components/kit'
import { DESKTOP_QUERY, useMediaQuery } from '@/components/shell/media'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { accountStatus, shortAccountLabel } from '@/lib/account'
import { useReturnFocus } from '@/lib/focus'
import { accountSubtitle, shortDate, statedAmount } from './bank'
import { CheckpointList } from './CheckpointList'

export function ReviewSheet({
  account,
  open,
  onOpenChange,
}: {
  account: Account
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const desktop = useMediaQuery(DESKTOP_QUERY)
  const title = account.type === 'brokerage' ? 'Revisar saldos' : 'Revisar con el banco'
  const returnFocus = useReturnFocus()
  const { openImport } = useImport()
  // "Importar estado" closes the sheet first and opens the import dialog once focus is
  // back on whatever opened the sheet, so the import dialog returns focus there too
  // (not to the sheet's button, gone by then).
  const importAfterClose = useRef(false)
  const description = `${shortAccountLabel(account)} · ${accountSubtitle(account)}`
  // Focus the sheet itself, not its first button: it opens on its own from
  // ?revisar=1, and a ring on "Importar estado" would read as a suggestion.
  const focus = {
    onOpenAutoFocus: (event: Event) => {
      returnFocus.onOpenAutoFocus()
      event.preventDefault()
      if (event.target instanceof HTMLElement) event.target.focus()
    },
    onCloseAutoFocus: (event: Event) => {
      returnFocus.onCloseAutoFocus(event)
      if (importAfterClose.current) {
        importAfterClose.current = false
        openImport({ accountId: account.id })
      }
    },
  }

  const body = (
    <div className="flex flex-col gap-6">
      <Summary
        account={account}
        onImport={() => {
          importAfterClose.current = true
          onOpenChange(false)
        }}
      />
      <CheckpointList account={account} />
      <ReconciliationPanel account={account} />
    </div>
  )

  if (desktop) {
    return (
      <Dialog open={open} onOpenChange={onOpenChange}>
        <DialogContent className="flex flex-col gap-0 p-0 sm:max-w-160" {...focus}>
          <DialogHeader className="px-6 pt-6 pb-4">
            <DialogTitle>{title}</DialogTitle>
            <DialogDescription className="text-small font-medium">
              {description}
            </DialogDescription>
          </DialogHeader>
          <div className="min-h-0 flex-1 overflow-y-auto px-6 pb-6">{body}</div>
        </DialogContent>
      </Dialog>
    )
  }

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent side="bottom" className="gap-0" {...focus}>
        <span
          aria-hidden="true"
          className="mx-auto mt-2 h-1 w-9 shrink-0 rounded-full bg-muted-foreground"
        />
        <SheetHeader className="pt-3">
          <SheetTitle>{title}</SheetTitle>
          <SheetDescription>{description}</SheetDescription>
        </SheetHeader>
        <div className="min-h-0 flex-1 overflow-y-auto px-4 pt-2 pb-4">{body}</div>
      </SheetContent>
    </Sheet>
  )
}

/**
 * What is off, since when, and what to do; or that it matches. The sheet's list right
 * below has "Anotar saldo" and each figure's "Usar como saldo inicial", so the
 * summary does not repeat them.
 */
function Summary({ account, onImport }: { account: Account; onImport: () => void }) {
  const status = accountStatus(account)
  const drift = account.drift
  const brokerage = account.type === 'brokerage'
  const source = brokerage ? 'Interactive Brokers' : 'El banco'

  if (status?.kind === 'no-cuadra' && drift) {
    return (
      <Notice
        title={
          <>
            No cuadra por <Amount amount={status.difference} size="inline" />
          </>
        }
        description={
          <>
            {source} dice <Amount amount={status.stated} size="inline" /> el{' '}
            {shortDate(status.asOf)} y tus movimientos suman{' '}
            <Amount amount={statedAmount(account, drift.computed)} size="inline" />.{' '}
            {brokerage
              ? 'Si falta un movimiento, sincroniza otra vez; si el saldo está mal anotado, bórralo.'
              : 'Si falta un movimiento, importa el estado de esos días; si el saldo está mal anotado, bórralo.'}
          </>
        }
        action={
          brokerage
            ? undefined
            : { label: 'Importar estado', icon: FileUpIcon, onClick: onImport }
        }
      />
    )
  }

  if (status?.kind === 'sin-partida') {
    return (
      <Notice
        tone="pending"
        icon={ScaleIcon}
        title="Esta cuenta no tiene saldo inicial"
        description="Elige uno de estos saldos como inicial o anota el tuyo. Luego te decimos si cuadra."
      />
    )
  }

  if (status?.kind === 'cuadra') {
    return (
      <p className="flex items-center gap-3 rounded-2xl bg-positive-soft p-4 text-body font-semibold">
        <CircleCheckIcon aria-hidden="true" className="size-6 shrink-0 text-positive" />
        Cuadra con {brokerage ? 'Interactive Brokers' : 'el banco'} al{' '}
        {shortDate(status.asOf)}.
      </p>
    )
  }

  return null
}
