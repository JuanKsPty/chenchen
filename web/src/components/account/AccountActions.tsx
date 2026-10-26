/**
 * The account's overflow menu (spec menu): Anotar compra, Anotar compras con Atajos,
 * Importar estado, Saldo según tu banco, Renombrar, a divider, Eliminar. A brokerage
 * account has no purchases, statements or bank figures to type, so it keeps
 * Renombrar and Eliminar.
 *
 *   const { items, dialogs } = useAccountActions(account)
 *   useTopBar({ …, menu: items })          the phone's "Más opciones"
 *   <AccountOverflow>{items}</AccountOverflow>   the ellipsis beside the desktop title
 *   {dialogs}                              rendered once by the page
 *
 * The menu items only flip state; the dialogs live outside the menu, so closing the
 * menu never unmounts the dialog it opened. On close each dialog gives focus back to
 * the menu's button (useReturnFocus resolves a menu item to its trigger).
 *
 * "Anotar compras con Atajos" is open exactly while the page's URL has `?atajo=1`
 * (lib/nav), as the review sheet is with `?revisar=1`: the menu item sets it, closing
 * clears it, and Ajustes › Conexiones links straight to it.
 */
import {
  EllipsisIcon,
  FileUpIcon,
  PencilIcon,
  ReceiptIcon,
  ScaleIcon,
  SmartphoneNfcIcon,
  Trash2Icon,
} from 'lucide-react'
import { useCallback, useMemo, useState, type ReactNode } from 'react'
import { useSearchParams } from 'react-router-dom'

import type { Account } from '@/api/types'
import { CheckpointDialog } from '@/components/CheckpointDialog'
import { useImport } from '@/components/import/ImportProvider'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { isShortcutRequested } from '@/lib/nav'
import { AddPurchaseDialog } from './AddPurchaseDialog'
import { DeleteAccountDialog } from './DeleteAccountDialog'
import { RenameAccountDialog } from './RenameAccountDialog'
import { ShortcutKeyDialog } from './ShortcutKeyDialog'

type Open = 'purchase' | 'checkpoint' | 'rename' | 'delete' | null

export function useAccountActions(account: Account): {
  items: ReactNode
  dialogs: ReactNode
  openCheckpoint: (anchor?: boolean) => void
} {
  const { openImport } = useImport()
  const [open, setOpen] = useState<Open>(null)
  const [params, setParams] = useSearchParams()
  const [anchor, setAnchor] = useState(false)
  const brokerage = account.type === 'brokerage'
  const accountId = account.id

  const setShortcut = useCallback(
    (show: boolean) =>
      setParams(
        (previous) => {
          const next = new URLSearchParams(previous)
          if (show) next.set('atajo', '1')
          else next.delete('atajo')
          return next
        },
        { replace: true },
      ),
    [setParams],
  )

  const items = useMemo(
    () => (
      <>
        {!brokerage && (
          <>
            <DropdownMenuItem onSelect={() => setOpen('purchase')}>
              <ReceiptIcon aria-hidden="true" />
              Anotar compra
            </DropdownMenuItem>
            <DropdownMenuItem onSelect={() => setShortcut(true)}>
              <SmartphoneNfcIcon aria-hidden="true" />
              Anotar compras con Atajos
            </DropdownMenuItem>
            <DropdownMenuItem onSelect={() => openImport({ accountId })}>
              <FileUpIcon aria-hidden="true" />
              Importar estado
            </DropdownMenuItem>
            <DropdownMenuItem
              onSelect={() => {
                setAnchor(false)
                setOpen('checkpoint')
              }}
            >
              <ScaleIcon aria-hidden="true" />
              Saldo según tu banco
            </DropdownMenuItem>
          </>
        )}
        <DropdownMenuItem onSelect={() => setOpen('rename')}>
          <PencilIcon aria-hidden="true" />
          Renombrar
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        <DropdownMenuItem variant="destructive" onSelect={() => setOpen('delete')}>
          <Trash2Icon aria-hidden="true" />
          Eliminar
        </DropdownMenuItem>
      </>
    ),
    [brokerage, accountId, openImport, setShortcut],
  )

  const close = (next: boolean) => {
    if (!next) setOpen(null)
  }

  const dialogs = (
    <>
      {!brokerage && (
        <>
          <AddPurchaseDialog
            account={account}
            open={open === 'purchase'}
            onOpenChange={close}
          />
          <CheckpointDialog
            account={account}
            anchor={anchor}
            open={open === 'checkpoint'}
            onOpenChange={close}
          />
          <ShortcutKeyDialog
            account={account}
            open={isShortcutRequested(params)}
            onOpenChange={setShortcut}
          />
        </>
      )}
      <RenameAccountDialog
        account={account}
        open={open === 'rename'}
        onOpenChange={close}
      />
      <DeleteAccountDialog
        account={account}
        open={open === 'delete'}
        onOpenChange={close}
      />
    </>
  )

  const openCheckpoint = (startAnchored = false) => {
    setAnchor(startAnchored)
    setOpen('checkpoint')
  }

  return { items, dialogs, openCheckpoint }
}

/** The 44px ellipsis beside the desktop title. Hidden below lg, where the bar has it. */
export function AccountOverflow({ children }: { children: ReactNode }) {
  return (
    // Non-modal, like the mobile bar's: an item opens a dialog from its onSelect.
    <DropdownMenu modal={false}>
      <DropdownMenuTrigger asChild>
        <Button variant="plain" size="icon" aria-label="Más opciones">
          <EllipsisIcon aria-hidden="true" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-auto min-w-67">
        {children}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
