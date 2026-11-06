/**
 * <UploadReviewDialog open onOpenChange />
 *
 * The statements Claude sent that did not enter on their own (ADR-0004), opened from
 * Inicio's "Revisa 1 importe que llegó desde Claude" (HomeNotices; HomePage renders
 * the dialog). Focus starts on its title, never on "Descartar". One at a time,
 * oldest first, in the import dialog's frame (a bottom sheet below lg) and with its
 * pieces, so a file looks the same however it arrived:
 *   1. "Llegó desde Claude": the file's name and when it came, why it waits (the
 *      upload's reason, in a neutral note) and what the file says (its dry run,
 *      CheckedOutcome). [Importar] is the one primary, enabled once the dry run is on
 *      screen: a file is never imported unseen. A dry run that failed says why and
 *      offers "Reintentar". "Descartar", a ghost, sets it aside and drops the file
 *      (Claude can send it again), then shows the next one, or closes when none is
 *      left.
 *   2. After Importar, the import's own result (ImportedOutcome), with "Listo" and,
 *      when another one waits, a ghost "Revisar el siguiente".
 * Importing here is the web's import: an account that is new is made now, the first
 * time, as from the import dialog. It refreshes money, and the upload leaves the list.
 *
 * Every opening starts again from the oldest one waiting. Closing while an import or
 * a dismissal runs lets it finish, and a toast says how it went; reopening before
 * then shows it running. A file already imported or dismissed elsewhere says so
 * ("Este archivo ya no está") instead of failing.
 */
import { CircleAlertIcon, InfoIcon } from 'lucide-react'
import { useEffect, useState, type ReactNode } from 'react'
import { toast } from 'sonner'

import { isNotFound } from '@/api/client'
import type { StatementUpload } from '@/api/types'
import {
  CheckedOutcome,
  ImportedOutcome,
  importHeadline,
  importNeedsStep,
} from '@/components/accounts/ImportOutcome'
import { ResponsiveDialogContent } from '@/components/accounts/ResponsiveDialog'
import { importFailureText } from '@/components/ImportStatementDialog'
import { DIALOG_TITLE, useDialogRound, useViewFocus } from '@/components/import/flow'
import { problemMessage } from '@/components/settings/problem'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Dialog } from '@/components/ui/dialog'
import { Skeleton } from '@/components/ui/skeleton'
import { Spinner } from '@/components/ui/spinner'
import { useReturnFocus } from '@/lib/focus'
import { formatDayAndTime } from '@/lib/format'
import {
  useAccounts,
  useDismissStatementUpload,
  useImportStatementUpload,
  useStatementUploadDryRun,
  useStatementUploads,
} from '@/lib/queries'

// A failed request had disabled its button, which drops the focus; it goes back
// there, next to the message.
const ACTION = (action: 'import' | 'dismiss') =>
  `[role="dialog"] [data-upload-action="${action}"]`

export function UploadReviewDialog({
  open,
  onOpenChange,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  // A new round on every opening, so the flow starts again from the oldest file; an
  // import or a dismissal still running keeps its round, and reopening shows it
  // finishing (never a new round on the same file with Importar enabled).
  const { round, isOpen, setBusy } = useDialogRound(open)

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <ReviewFlow
        key={round}
        isOpen={isOpen}
        close={() => onOpenChange(false)}
        onBusyChange={setBusy}
      />
    </Dialog>
  )
}

type View = 'review' | 'imported'

function ReviewFlow({
  isOpen,
  close,
  onBusyChange,
}: {
  isOpen: React.RefObject<boolean>
  close: () => void
  onBusyChange: (busy: boolean) => void
}) {
  const uploads = useStatementUploads()
  const accounts = useAccounts()
  const list = accounts.data?.accounts ?? []
  const importUpload = useImportStatementUpload()
  const dismissUpload = useDismissStatementUpload()
  const returnFocus = useReturnFocus()

  const [view, setView] = useState<View>('review')
  // Answered in this opening; the list may still have them until it refreshes.
  const [handled, setHandled] = useState<string[]>([])
  // The one on screen, pinned once shown: the list drops it as soon as it is
  // imported, and its result must stay.
  const [current, setCurrent] = useState<StatementUpload | null>(null)
  const waiting = (uploads.data?.uploads ?? []).filter((u) => !handled.includes(u.id))
  if (current === null && view === 'review' && waiting[0]) setCurrent(waiting[0])
  const next = waiting.find((u) => u.id !== current?.id)

  const dryRun = useStatementUploadDryRun(current?.id ?? '', {
    enabled: current !== null && view === 'review',
  })
  const busy = importUpload.isPending || dismissUpload.isPending
  useEffect(() => {
    onBusyChange(busy)
  }, [busy, onBusyChange])
  // Not there anymore: imported or dismissed elsewhere.
  const gone = [dryRun.error, importUpload.error, dismissUpload.error].some(isNotFound)

  const shown = view === 'imported' ? 'imported' : `review:${current?.id ?? ''}`
  const focusOn = useViewFocus(shown)

  /** On to the next one waiting, or, with none left, closed. */
  const goTo = (after: StatementUpload | undefined) => {
    if (!after) {
      close()
      return
    }
    if (current) setHandled((ids) => [...ids, current.id])
    setCurrent(after)
    setView('review')
    importUpload.reset()
    dismissUpload.reset()
    focusOn(`review:${after.id}`, DIALOG_TITLE)
  }

  const runImport = () => {
    if (!current || busy) return
    const upload = current
    dismissUpload.reset()
    importUpload.mutate(upload.id, {
      onSuccess: (result) => {
        focusOn('imported', DIALOG_TITLE)
        setView('imported')
        if (!isOpen.current) toast(importHeadline(result))
      },
      onError: (error) => {
        if (isOpen.current) {
          focusOn(`review:${upload.id}`, ACTION('import'))
          return
        }
        toast(`No pudimos importar ${upload.filename}. ${importFailureText(error)}`)
      },
    })
  }

  const runDismiss = () => {
    if (!current || busy) return
    const upload = current
    const after = next
    importUpload.reset()
    dismissUpload.mutate(upload.id, {
      onSuccess: () => {
        toast('Listo: archivo descartado.')
        goTo(after)
      },
      onError: (error) => {
        if (isOpen.current) {
          focusOn(`review:${upload.id}`, ACTION('dismiss'))
          return
        }
        toast(`No pudimos descartar ${upload.filename}. ${problemMessage(error)}`)
      },
    })
  }

  const retryDryRun = () => {
    if (!current) return
    // "Reintentar" leaves with the failure it answers: the title takes the focus.
    focusOn(`review:${current.id}`, DIALOG_TITLE)
    void dryRun.refetch()
  }

  let title: ReactNode = 'Llegó desde Claude'
  let description: ReactNode
  let body: ReactNode
  let footer: ReactNode

  const nextButton = next && (
    <Button variant="ghost" size="lg" className="lg:h-11" onClick={() => goTo(next)}>
      Revisar el siguiente
    </Button>
  )

  if (view === 'imported' && importUpload.data) {
    const stepDown = importNeedsStep(importUpload.data, list)
    title = undefined
    body = <ImportedOutcome result={importUpload.data} accounts={list} onNavigate={close} />
    footer = (
      <>
        {nextButton}
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
  } else if (current && gone) {
    title = 'Este archivo ya no está'
    description = 'Ya lo importaste o lo descartaste, quizá desde otra pestaña.'
    footer = (
      <>
        {nextButton}
        <Button size="lg" className="lg:h-11" onClick={close}>
          Listo
        </Button>
      </>
    )
  } else if (current) {
    description = (
      <>
        <span className="break-all">{current.filename}</span>, el{' '}
        {formatDayAndTime(current.created_at)}. Todavía no importamos nada.
      </>
    )
    body = (
      <>
        {current.reason && (
          // A note, not an alert: nothing is wrong yet, it only waits for a look.
          <Alert role="note">
            <InfoIcon aria-hidden="true" />
            <AlertTitle>Por qué no entró solo</AlertTitle>
            <AlertDescription className="text-pretty">{current.reason}</AlertDescription>
          </Alert>
        )}
        {dryRun.isPending ? (
          <div aria-hidden="true" className="flex flex-col gap-3 rounded-2xl bg-muted p-4">
            <Skeleton className="h-4 w-40" />
            <Skeleton className="h-3.5 w-28" />
            <Skeleton className="h-3.5 w-full" />
            <Skeleton className="h-3.5 w-full" />
          </div>
        ) : dryRun.isError ? (
          <Failure
            title="No pudimos leer el archivo"
            text={importFailureText(dryRun.error)}
          />
        ) : (
          <CheckedOutcome report={dryRun.data} accounts={list} />
        )}
        {importUpload.error && (
          <Failure
            title="No pudimos importar el archivo"
            text={importFailureText(importUpload.error)}
          />
        )}
        {dismissUpload.error && (
          <Failure
            title="No pudimos descartar el archivo"
            text={problemMessage(dismissUpload.error)}
          />
        )}
      </>
    )
    footer = (
      <>
        <Button
          variant="ghost"
          size="lg"
          className="lg:h-11"
          data-upload-action="dismiss"
          disabled={busy}
          onClick={runDismiss}
        >
          {dismissUpload.isPending ? (
            <>
              <Spinner aria-hidden="true" />
              Descartando…
            </>
          ) : (
            'Descartar'
          )}
        </Button>
        {dryRun.isError && (
          <Button
            variant="secondary"
            size="lg"
            className="lg:h-11"
            disabled={busy}
            onClick={retryDryRun}
          >
            Reintentar
          </Button>
        )}
        <Button
          size="lg"
          className="lg:h-11"
          data-upload-action="import"
          // What the file says comes first: no import before it is on screen, and none
          // when it could not be read.
          disabled={busy || !dryRun.isSuccess}
          onClick={runImport}
        >
          {importUpload.isPending ? (
            <>
              <Spinner aria-hidden="true" />
              Importando…
            </>
          ) : (
            'Importar'
          )}
        </Button>
      </>
    )
  } else if (uploads.isPending) {
    body = (
      <p role="status" className="flex items-center gap-2 text-body text-muted-foreground">
        <Spinner aria-hidden="true" />
        Un momento…
      </p>
    )
  } else if (uploads.isError) {
    title = 'No pudimos cargar lo que llegó'
    description = problemMessage(uploads.error)
    footer = (
      <Button size="lg" className="lg:h-11" onClick={() => void uploads.refetch()}>
        Reintentar
      </Button>
    )
  } else {
    title = 'No queda nada por revisar'
    description = 'Lo que llegó desde Claude ya está en orden.'
    footer = (
      <Button size="lg" className="lg:h-11" onClick={close}>
        Listo
      </Button>
    )
  }

  return (
    <ResponsiveDialogContent
      title={title}
      description={description}
      footer={footer}
      onOpenAutoFocus={(event) => {
        returnFocus.onOpenAutoFocus()
        // The title first, never "Descartar": an Enter on opening reads the dialog
        // instead of throwing a file away.
        const heading = document.querySelector<HTMLElement>(DIALOG_TITLE)
        if (heading) {
          event.preventDefault()
          heading.focus()
        }
      }}
      onCloseAutoFocus={returnFocus.onCloseAutoFocus}
    >
      {body}
    </ResponsiveDialogContent>
  )
}

function Failure({ title, text }: { title: string; text: string }) {
  return (
    <Alert variant="attention">
      <CircleAlertIcon aria-hidden="true" />
      <AlertTitle>{title}</AlertTitle>
      <AlertDescription>{text}</AlertDescription>
    </Alert>
  )
}
