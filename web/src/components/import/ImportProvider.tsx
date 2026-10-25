/**
 * One import dialog for the whole signed-in app.
 *
 * Frozen interface (F6):
 *
 *   const { openImport } = useImport()
 *   openImport()                         import any file; the file says which account
 *   openImport({ accountId: account.id }) locked to that account (an account's page)
 *
 * The desktop bar's "Importar estado", the mobile Cuentas header, an account's
 * overflow menu and every empty state call this instead of rendering a dialog of
 * their own, so there is exactly one <ImportStatementDialog> and it is controlled
 * here: ImportStatementDialog({ open, onOpenChange, accountId? }).
 *
 * <ImportProvider> sits inside the AppShell; useImport() outside it throws.
 */
import { createContext, useCallback, useContext, useMemo, useRef, useState } from 'react'

import { ImportStatementDialog } from '@/components/ImportStatementDialog'
import { rememberFocus } from '@/lib/focus'

export interface OpenImportOptions {
  /** Lock the import to this account: a file for any other account is refused. */
  accountId?: string
}

interface ImportContextValue {
  openImport: (options?: OpenImportOptions) => void
}

const ImportContext = createContext<ImportContextValue | null>(null)

export function useImport(): ImportContextValue {
  const value = useContext(ImportContext)
  if (!value) throw new Error('useImport must be used inside an ImportProvider')
  return value
}

export function ImportProvider({ children }: { children: React.ReactNode }) {
  const [open, setOpen] = useState(false)
  // Kept after closing, so the dialog's text does not change while it fades out.
  const [accountId, setAccountId] = useState<string | undefined>(undefined)
  // The dialog mounts the first time it is asked for, not on every page load.
  const [requested, setRequested] = useState(false)
  // Whatever opened it gets the focus back on close: the dialog has no Radix trigger
  // to return to, so it would otherwise land on <body>. Opened from a menu item, that
  // is the menu's button (rememberFocus), since the item unmounts with the menu.
  const returnFocus = useRef<HTMLElement | null>(null)

  const openImport = useCallback((options: OpenImportOptions = {}) => {
    returnFocus.current = rememberFocus()
    setAccountId(options.accountId)
    setRequested(true)
    setOpen(true)
  }, [])

  const changeOpen = useCallback((next: boolean) => {
    setOpen(next)
    const element = returnFocus.current
    if (next || !element) return
    returnFocus.current = null
    requestAnimationFrame(() => {
      if (element.isConnected) element.focus()
    })
  }, [])

  const value = useMemo(() => ({ openImport }), [openImport])

  return (
    <ImportContext.Provider value={value}>
      {children}
      {requested && (
        <ImportStatementDialog
          open={open}
          onOpenChange={changeOpen}
          accountId={accountId}
        />
      )}
    </ImportContext.Provider>
  )
}
