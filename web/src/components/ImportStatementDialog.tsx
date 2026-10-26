/**
 * Importing a statement file.
 *
 * Signature (frozen, F6): ImportStatementDialog({ open, onOpenChange, accountId? }).
 * Controlled: the shell's ImportProvider renders the only instance; screens open it
 * with useImport().openImport({ accountId? }) and never render it themselves.
 *
 * Which account a file belongs to is never chosen by hand: the file says, and the
 * account is created the first time it is seen. Opened from an account's own page,
 * the dialog is locked to that account, and a file for any other account is refused
 * instead of being filed somewhere unexpected.
 *
 * The flow, in one dialog (a bottom sheet below lg):
 *   1. Pick a .txt, .csv or .xlsx (button or drop). Then either
 *      "Importar" (primary), or "Solo revisar" (ghost): a dry run that shows what
 *      the file says without writing anything, with "Importar ahora" after it.
 *   2. The result, per voice.importResult: "Listo: 12 movimientos nuevos …",
 *      the bank's balances and the account's chip. A card file with no balance
 *      offers "Saldo según tu banco" right there.
 * Every opening starts again at step 1. Closing while an import runs lets it
 * finish, and a toast says how it went; reopening before then shows it running.
 */
import { CircleAlertIcon } from 'lucide-react'
import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { toast } from 'sonner'

import { ApiError } from '@/api/client'
import type { Account } from '@/api/types'
import {
  accountName,
  CheckedOutcome,
  ImportedOutcome,
  importHeadline,
  importNeedsStep,
  matchAccount,
} from '@/components/accounts/ImportOutcome'
import { ImportFilePicker } from '@/components/accounts/ImportFilePicker'
import { ResponsiveDialogContent } from '@/components/accounts/ResponsiveDialog'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Dialog } from '@/components/ui/dialog'
import { Spinner } from '@/components/ui/spinner'
import { useAccounts, useDryRunStatement, useImportStatement } from '@/lib/queries'

// A view change replaces the button that had the focus: move it to the new
// view's title (or back to the picker), so a screen reader hears the result.
const TITLE = '[role="dialog"] [data-slot="dialog-title"]'
const PICKER = '[role="dialog"] [data-import-picker]'
// A running import or check disables its button, which drops the focus; when it
// fails, the focus goes back to that button, next to the message.
const ACTION = (action: 'import' | 'check') =>
  `[role="dialog"] [data-import-action="${action}"]`

const GENERIC_ERROR =
  'Algo falló de nuestro lado. Tus datos están bien; intenta otra vez en un momento.'

export function ImportStatementDialog({
  open,
  onOpenChange,
  accountId,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** Lock the import to this account. */
  accountId?: string
}) {
  // A new session on every opening: the flow below remounts and starts clean,
  // while a closing dialog keeps showing what it showed as it fades out. An import
  // still running keeps its flow instead: reopened, the dialog shows that import
  // finishing (and how it went), not a fresh picker that could start another.
  const [session, setSession] = useState(0)
  const [sessionAccountId, setSessionAccountId] = useState(accountId)
  const [importing, setImporting] = useState(false)
  const [wasOpen, setWasOpen] = useState(open)
  if (open !== wasOpen) {
    setWasOpen(open)
    if (open && !importing) {
      setSession((n) => n + 1)
      setSessionAccountId(accountId)
    }
  }
  const isOpen = useRef(open)
  useEffect(() => {
    isOpen.current = open
  }, [open])

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <ImportFlow
        key={session}
        accountId={sessionAccountId}
        isOpen={isOpen}
        close={() => onOpenChange(false)}
        onImportingChange={setImporting}
      />
    </Dialog>
  )
}

type View = 'pick' | 'checked' | 'imported'

function ImportFlow({
  accountId,
  isOpen,
  close,
  onImportingChange,
}: {
  accountId?: string
  isOpen: React.RefObject<boolean>
  close: () => void
  onImportingChange: (importing: boolean) => void
}) {
  const [file, setFile] = useState<File | null>(null)
  const [view, setView] = useState<View>('pick')
  const upload = useImportStatement()
  const check = useDryRunStatement()
  useEffect(() => {
    onImportingChange(upload.isPending)
  }, [upload.isPending, onImportingChange])
  const accounts = useAccounts()
  const list = accounts.data?.accounts ?? []
  const lockedTo = accountId ? list.find((a) => a.id === accountId) : undefined
  const busy = upload.isPending || check.isPending

  // The view that is actually on screen (a result view needs its data too).
  const shown: View =
    view === 'imported' && upload.data
      ? 'imported'
      : view === 'checked' && check.data
        ? 'checked'
        : 'pick'

  // Focus moves once the new view is in the DOM, in the same commit, and only
  // when that view is the one asked for (never the old view's title).
  const pendingFocus = useRef<{ view: View; selector: string } | null>(null)
  const focusOn = (next: View, selector: string) => {
    pendingFocus.current = { view: next, selector }
  }
  useLayoutEffect(() => {
    const request = pendingFocus.current
    if (!request || request.view !== shown) return
    pendingFocus.current = null
    document.querySelector<HTMLElement>(request.selector)?.focus()
  })

  const choose = (next: File) => {
    setFile(next)
    setView('pick')
    upload.reset()
    check.reset()
  }

  const runImport = () => {
    if (!file) return
    // A finished check stays on screen while "Importar ahora" runs; a failed
    // one is cleared so its message does not linger.
    if (check.isError) check.reset()
    upload.mutate(
      { file, accountId },
      {
        onSuccess: (result) => {
          focusOn('imported', TITLE)
          setView('imported')
          if (!isOpen.current) toast(importHeadline(result))
        },
        onError: (error) => {
          if (isOpen.current) {
            focusOn(view, ACTION('import'))
            return
          }
          const failure = failureText(error, lockedTo)
          toast(`No pudimos importar ${file.name}. ${failure}`)
        },
      },
    )
  }

  const runCheck = () => {
    if (!file) return
    upload.reset()
    check.mutate(file, {
      onSuccess: () => {
        focusOn('checked', TITLE)
        setView('checked')
      },
      onError: () => focusOn('pick', ACTION('check')),
    })
  }

  const startOver = () => {
    focusOn('pick', PICKER)
    setFile(null)
    setView('pick')
    upload.reset()
    check.reset()
  }

  const importing = upload.isPending && (
    <>
      <Spinner aria-hidden="true" />
      Importando…
    </>
  )

  let body: React.ReactNode
  let footer: React.ReactNode
  // The result draws its own headline as the dialog's title.
  let title: string | undefined = 'Importar estado de cuenta'
  let description: React.ReactNode = (
    <Instructions accountId={accountId} lockedTo={lockedTo} />
  )

  if (shown === 'imported' && upload.data) {
    const stepDown = importNeedsStep(upload.data, list)
    title = undefined
    description = undefined
    body = <ImportedOutcome result={upload.data} accounts={list} onNavigate={close} />
    footer = (
      <>
        <Button variant="ghost" size="lg" className="lg:h-11" onClick={startOver}>
          Importar otro
        </Button>
        <Button
          variant={stepDown ? 'secondary' : 'default'}
          size="lg"
          className="lg:h-11"
          onClick={close}
        >
          Listo
        </Button>
      </>
    )
  } else if (shown === 'checked' && check.data) {
    const elsewhere = Boolean(
      lockedTo &&
      check.data.statements.some((s) => matchAccount(s, list)?.id !== lockedTo.id),
    )
    title = 'Esto dice el archivo'
    description = 'Todavía no importamos nada.'
    body = (
      <>
        <CheckedOutcome report={check.data} accounts={list} lockedTo={lockedTo} />
        <Failure upload={upload.error} lockedTo={lockedTo} />
      </>
    )
    footer = (
      <>
        <Button
          variant="ghost"
          size="lg"
          className="lg:h-11"
          disabled={busy}
          onClick={startOver}
        >
          Elegir otro archivo
        </Button>
        <Button
          size="lg"
          className="lg:h-11"
          data-import-action="import"
          disabled={busy || elsewhere}
          onClick={runImport}
        >
          {importing || 'Importar ahora'}
        </Button>
      </>
    )
  } else {
    body = (
      <>
        <ImportFilePicker file={file} onFile={choose} disabled={busy} />
        <Failure upload={upload.error} check={check.error} lockedTo={lockedTo} />
      </>
    )
    footer = (
      <>
        <Button
          variant="ghost"
          size="lg"
          className="lg:h-11"
          data-import-action="check"
          disabled={!file || busy}
          onClick={runCheck}
        >
          {check.isPending ? (
            <>
              <Spinner aria-hidden="true" />
              Revisando…
            </>
          ) : (
            'Solo revisar'
          )}
        </Button>
        <Button
          size="lg"
          className="lg:h-11"
          data-import-action="import"
          disabled={!file || busy}
          onClick={runImport}
        >
          {importing || 'Importar'}
        </Button>
      </>
    )
  }

  return (
    <ResponsiveDialogContent title={title} description={description} footer={footer}>
      {body}
    </ResponsiveDialogContent>
  )
}

function Instructions({ accountId, lockedTo }: { accountId?: string; lockedTo?: Account }) {
  return (
    <>
      Sube el estado de cuenta que te da tu banco (.csv, .txt o .xlsx). Si un movimiento ya
      estaba, no lo repetimos.
      {accountId && lockedTo && (
        <>
          {' '}
          Solo para{' '}
          <span className="font-semibold text-foreground">{accountName(lockedTo)}</span>.
        </>
      )}
    </>
  )
}

function Failure({
  upload,
  check,
  lockedTo,
}: {
  upload?: Error | null
  check?: Error | null
  lockedTo?: Account
}) {
  const error = upload ?? check
  if (!error) return null
  return (
    <Alert variant="attention">
      <CircleAlertIcon />
      <AlertTitle>
        {upload ? 'No pudimos importar el archivo' : 'No pudimos leer el archivo'}
      </AlertTitle>
      <AlertDescription>{failureText(error, lockedTo)}</AlertDescription>
    </Alert>
  )
}

/**
 * What went wrong, in our words. The known codes get their own copy, worded for
 * this dialog; an unreadable statement keeps the line number the server names
 * ("La línea 8 del archivo…", or "line 8" from an older server). Anything else the
 * server says in a 4xx is shown as it comes, and a 5xx or a network failure is the
 * generic line.
 */
function failureText(error: Error, lockedTo?: Account): string {
  if (!(error instanceof ApiError)) return GENERIC_ERROR
  switch (error.code) {
    case 'unrecognised_format':
      return 'No reconocemos este archivo. Sube el estado de cuenta tal como te lo da tu banco (.csv, .txt o .xlsx).'
    case 'unreadable_statement': {
      const line = /\b(?:line|l[ií]nea) (\d+)/i.exec(error.message)?.[1]
      return line
        ? `La línea ${line} tiene un formato que no reconocemos. Descarga el archivo otra vez de tu banco y súbelo sin abrirlo ni editarlo.`
        : 'Tiene un formato que no reconocemos. Descárgalo otra vez de tu banco y súbelo sin abrirlo ni editarlo.'
    }
    case 'account_mismatch':
      return lockedTo
        ? `Este archivo es de otra cuenta. Desde aquí solo importas a ${accountName(lockedTo)}.`
        : error.message
    case 'upload_too_large_or_malformed':
      return 'El archivo pesa más de 20 MB. Revisa que sea tu estado de cuenta.'
    case 'missing_file':
    case 'upload_unreadable':
      return 'No pudimos leer el archivo. Elígelo otra vez.'
    default:
      return error.status < 500 && error.message ? error.message : GENERIC_ERROR
  }
}
