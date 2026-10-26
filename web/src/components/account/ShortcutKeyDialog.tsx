/**
 * <ShortcutKeyDialog account open onOpenChange />
 *
 * "Anotar compras con Atajos": the key this account's iPhone shortcut notes purchases
 * with, and how to set that shortcut up. Opened from the account's overflow menu
 * (never a brokerage account's) and by `?atajo=1` on its page, which Ajustes ›
 * Conexiones links to; useAccountActions keeps it open exactly while the URL says so.
 * Below lg a bottom sheet, from lg a dialog (ResponsiveDialogContent).
 *
 * Three states, from the account's `shortcut` and from what this opening did:
 *   no key     what it does and what it cannot ("Solo pagos con el iPhone en tienda",
 *              "No deja ver nada"), with [Crear clave] as the one primary.
 *   just made  "Copia esta clave ahora", the Dirección and the Clave, each with
 *              [Copiar], and [Listo].
 *   a key      "Clave ···x7Qa", when it was made and how it is doing, the Dirección,
 *              and [Crear otra clave] / [Desactivar], each confirmed first in the
 *              inverse dialog RemovePinDialog uses.
 * All three end with "Obtener el atajo", when the build has a ready-made shortcut
 * (VITE_SHORTCUT_URL), and the steps under "¿Cómo lo configuro?".
 *
 * The key is a secret shown once. It lives only in this dialog's state: never in the
 * URL, in storage, in a toast or a log, nor in the query cache (the mutation that
 * brings it is reset at once and kept for no time, useCreateShortcutKey). Closing
 * the dialog forgets it, however it closes, and every opening starts from what the
 * server says. A key that arrives after the dialog closed is dropped unseen: the
 * account then shows a key, untested, and "Crear otra clave" makes one to copy.
 *
 * One request about the key at a time, and a confirmation stays until its request is
 * answered. The server takes a new key and a revoke in whatever order they arrive,
 * so two at once could leave on screen a key the other one had already turned off.
 */
import {
  ChevronDownIcon,
  CircleAlertIcon,
  CopyIcon,
  ExternalLinkIcon,
  KeyRoundIcon,
} from 'lucide-react'
import { useEffect, useId, useRef, useState, type ReactNode } from 'react'
import { toast } from 'sonner'

import { SHORTCUT_PURCHASES_PATH } from '@/api/endpoints'
import type { Account, ShortcutStatus } from '@/api/types'
import { ResponsiveDialogContent } from '@/components/accounts/ResponsiveDialog'
import { Tile } from '@/components/kit'
import { focusWhenReady } from '@/components/settings/focus'
import { problemMessage } from '@/components/settings/problem'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Field, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Spinner } from '@/components/ui/spinner'
import { shortcutNote } from '@/lib/account'
import { useReturnFocus } from '@/lib/focus'
import { formatDay } from '@/lib/format'
import { useCreateShortcutKey, useRevokeShortcutKey } from '@/lib/queries'

const INTRO =
  'Cada compra que pagues acercando el iPhone llega aquí al momento. Cuando llegue tu estado, la cuadramos sola.'

const COPIED = 'Listo: copiada'
const NOT_COPIED = 'No pudimos copiarla. Ya está seleccionada: cópiala tú.'

/**
 * The ready-made shortcut to install, when the build was given one. Only an https
 * link: it becomes an href.
 */
const TEMPLATE_URL = httpsUrl(import.meta.env.VITE_SHORTCUT_URL)

function httpsUrl(value: string | undefined): string | null {
  if (!value) return null
  try {
    const url = new URL(value)
    return url.protocol === 'https:' ? url.href : null
  } catch {
    return null
  }
}

export function ShortcutKeyDialog({
  account,
  open,
  onOpenChange,
}: {
  account: Account
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  // Every opening starts afresh from what the server says, never from a key made
  // before. Counted on opening, not on closing, so the dialog keeps its look while
  // it animates out.
  const [round, setRound] = useState(0)
  const [wasOpen, setWasOpen] = useState(open)
  if (open !== wasOpen) {
    setWasOpen(open)
    if (open) setRound((n) => n + 1)
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <KeySetup
        key={round}
        account={account}
        open={open}
        onDone={() => onOpenChange(false)}
      />
    </Dialog>
  )
}

type Confirming = 'replace' | 'revoke' | null

function KeySetup({
  account,
  open,
  onDone,
}: {
  account: Account
  open: boolean
  onDone: () => void
}) {
  const ids = useId()
  const madeId = `${ids}-hecha`
  const bodyRef = useRef<HTMLDivElement>(null)
  const returnFocus = useReturnFocus()
  const create = useCreateShortcutKey(account.id)
  const revoke = useRevokeShortcutKey(account.id)

  // Whether this opening made a key, and the key itself.
  const [made, setMade] = useState(false)
  const [secret, setSecret] = useState<string | null>(null)
  const [confirming, setConfirming] = useState<Confirming>(null)
  // Closed, the key is forgotten at once, whichever way the dialog closed (and
  // one that arrives after it closed is never kept). The view stays "just made",
  // with empty fields, while the dialog animates out.
  if (!open && secret !== null) setSecret(null)

  const shortcut = account.shortcut
  const view = made ? 'made' : shortcut?.configured ? 'key' : 'none'
  // Either request still running: neither may start until it is answered.
  const busy = create.isPending || revoke.isPending

  const madeLine = () => document.getElementById(madeId)

  /**
   * Makes the key and shows it, with focus on "Copia esta clave ahora" once it is on
   * screen; from a confirmation, `done` closes that first and focuses from there.
   */
  const makeKey = (done?: ConfirmDone) => {
    if (busy) return
    create.mutate(undefined, {
      onSuccess: (newKey) => {
        setSecret(newKey.key)
        setMade(true)
        // Out of the mutation cache now: from here on the key lives only above.
        create.reset()
        if (done) done(madeLine)
        else focusWhenReady(madeLine)
      },
    })
  }

  const revokeKey = (done: ConfirmDone) => {
    if (busy) return
    revoke.mutate(undefined, {
      onSuccess: () => {
        toast('Desactivaste la clave.')
        // "Desactivar" went away with the key: focus the dialog itself, which reads
        // its title and what it does again.
        done(() => bodyRef.current?.closest<HTMLElement>('[role="dialog"]'))
      },
    })
  }

  const address = `${window.location.origin}${SHORTCUT_PURCHASES_PATH}`

  return (
    <ResponsiveDialogContent
      title="Anotar compras con Atajos"
      description={view === 'made' ? undefined : INTRO}
      onOpenAutoFocus={(event) => {
        returnFocus.onOpenAutoFocus()
        // The dialog itself, not its first button: it also opens on its own from
        // ?atajo=1, and a ring on "Crear clave" would read as a suggestion.
        event.preventDefault()
        if (event.target instanceof HTMLElement) event.target.focus()
      }}
      onCloseAutoFocus={returnFocus.onCloseAutoFocus}
      footer={
        view === 'none' ? (
          <>
            <DialogClose asChild>
              <Button variant="ghost" size="lg" className="lg:h-11">
                Cancelar
              </Button>
            </DialogClose>
            <Button
              size="lg"
              className="lg:h-11"
              // Not disabled: that would drop the focus while it works, and an
              // error would then leave it nowhere.
              aria-disabled={busy || undefined}
              onClick={() => makeKey()}
            >
              {create.isPending && <Spinner aria-hidden="true" />}
              Crear clave
            </Button>
          </>
        ) : view === 'made' ? (
          <Button size="lg" className="lg:h-11" onClick={onDone}>
            Listo
          </Button>
        ) : undefined
      }
    >
      <div ref={bodyRef} className="flex flex-col gap-4">
        {view === 'none' && (
          <>
            <p className="text-body text-pretty">
              La clave solo sirve para anotar compras en esta cuenta. No deja ver nada.
            </p>
            <p className="text-small font-medium text-pretty text-muted-foreground">
              Solo pagos con el iPhone en tienda. Lo que pagues en línea o con la tarjeta
              física llega con tu estado.
            </p>
            {/* Here rather than at the end, where a phone would scroll it out of sight. */}
            {create.isError && (
              <Alert variant="attention">
                <CircleAlertIcon aria-hidden="true" />
                <AlertTitle>No pudimos crear la clave</AlertTitle>
                <AlertDescription>{problemMessage(create.error)}</AlertDescription>
              </Alert>
            )}
          </>
        )}

        {view === 'made' && (
          <>
            <p
              id={madeId}
              tabIndex={-1}
              className="text-body font-semibold text-pretty outline-none"
            >
              Copia esta clave ahora: no la volvemos a mostrar.
            </p>
            <CopyField label="Dirección" copyLabel="Copiar dirección" value={address} />
            <CopyField label="Clave" copyLabel="Copiar clave" value={secret ?? ''} />
          </>
        )}

        {view === 'key' && shortcut && (
          <>
            <KeyRow
              shortcut={shortcut}
              busy={busy}
              onReplace={() => setConfirming('replace')}
              onRevoke={() => setConfirming('revoke')}
            />
            <CopyField label="Dirección" copyLabel="Copiar dirección" value={address} />
          </>
        )}

        {TEMPLATE_URL && (
          <Button variant="secondary" asChild className="self-start">
            <a href={TEMPLATE_URL} target="_blank" rel="noreferrer">
              Obtener el atajo
              <span className="sr-only">, se abre en otra pestaña</span>
              <ExternalLinkIcon aria-hidden="true" data-icon="inline-end" />
            </a>
          </Button>
        )}

        <SetupSteps
          card={
            account.type === 'credit_card' ? 'esta tarjeta' : 'la tarjeta de esta cuenta'
          }
        />

        {/* A confirmation never closes while its request runs (ConfirmDialog), so
            closing one only ever clears a failed attempt, and the next opening
            starts clean. */}
        <ConfirmDialog
          open={confirming === 'replace'}
          onOpenChange={(next) => {
            if (next) return
            setConfirming(null)
            create.reset()
          }}
          title="¿Crear otra clave?"
          description="La anterior deja de funcionar y tendrás que pegar la nueva en el atajo."
          action="Crear otra clave"
          pending={create.isPending}
          failure={create.error}
          onConfirm={makeKey}
        />
        <ConfirmDialog
          open={confirming === 'revoke'}
          onOpenChange={(next) => {
            if (next) return
            setConfirming(null)
            revoke.reset()
          }}
          title="¿Desactivar la clave?"
          description="El atajo de esta cuenta deja de anotar compras."
          action="Desactivar clave"
          pending={revoke.isPending}
          failure={revoke.error}
          onConfirm={revokeKey}
        />
      </div>
    </ResponsiveDialogContent>
  )
}

/** The key there is: its last four, when it was made, how it is doing, and what to do. */
function KeyRow({
  shortcut,
  busy,
  onReplace,
  onRevoke,
}: {
  shortcut: ShortcutStatus
  /** A request about the key is running: neither button starts another. */
  busy: boolean
  onReplace: () => void
  onRevoke: () => void
}) {
  const made = shortcut.created_at ? `Creada el ${formatDay(shortcut.created_at)} · ` : ''
  return (
    <div className="grid grid-cols-[2.5rem_minmax(0,1fr)] items-center gap-x-3 gap-y-2">
      <Tile icon={KeyRoundIcon} />
      <div className="flex min-w-0 flex-col">
        <h3 className="text-body font-semibold">Clave ···{shortcut.hint}</h3>
        <p className="text-small font-medium text-pretty text-muted-foreground">
          {made}
          {shortcutNote(shortcut)}
        </p>
      </div>
      {/* Under the text, in its column; the ghost's label lines up with it. */}
      <div className="col-start-2 -ml-3 flex flex-wrap gap-2">
        <Button variant="ghost" disabled={busy} onClick={onReplace}>
          Crear otra clave
        </Button>
        <Button variant="plain" disabled={busy} onClick={onRevoke}>
          Desactivar
        </Button>
      </div>
    </div>
  )
}

/**
 * A read-only field with its own [Copiar]. The text can always be selected by hand
 * too: tapping the field selects all of it.
 */
function CopyField({
  label,
  copyLabel,
  value,
}: {
  label: string
  /** The button's name for a screen reader: "Copiar clave". */
  copyLabel: string
  value: string
}) {
  const id = useId()
  const inputRef = useRef<HTMLInputElement>(null)
  return (
    <Field>
      <FieldLabel htmlFor={id}>{label}</FieldLabel>
      <div className="flex gap-2">
        <Input
          ref={inputRef}
          id={id}
          value={value}
          readOnly
          autoComplete="off"
          autoCapitalize="off"
          autoCorrect="off"
          spellCheck={false}
          className="min-w-0 flex-1"
          onFocus={(event) => selectAll(event.currentTarget)}
        />
        <Button
          type="button"
          variant="secondary"
          size="lg"
          className="md:h-11"
          aria-label={copyLabel}
          onClick={() => copyFrom(inputRef.current)}
        >
          <CopyIcon aria-hidden="true" />
          Copiar
        </Button>
      </div>
    </Field>
  )
}

/**
 * Copies a field's text. The Clipboard API first, called straight from the click so
 * Safari counts it as the person's own gesture and asks for no permission. Where it
 * is missing (a page served over plain http) or refuses, the text is selected in its
 * field and the older copy command tried; if that fails too, the selection stays for
 * the person to copy, and the toast says so. Neither toast repeats the text.
 */
function copyFrom(input: HTMLInputElement | null) {
  if (!input) return
  const fallback = () => {
    selectAll(input)
    let copied = false
    try {
      copied = document.execCommand('copy')
    } catch {
      copied = false
    }
    toast(copied ? COPIED : NOT_COPIED)
  }
  if (typeof navigator.clipboard?.writeText === 'function') {
    navigator.clipboard.writeText(input.value).then(() => toast(COPIED), fallback)
  } else {
    fallback()
  }
}

/** Selects all of a field's text. iOS Safari ignores select() alone. */
function selectAll(input: HTMLInputElement) {
  if (document.activeElement !== input) input.focus({ preventScroll: true })
  input.select()
  input.setSelectionRange(0, input.value.length)
}

/**
 * What a confirmation's request calls once it went through: it closes the
 * confirmation and, once that is gone, puts focus on `target`.
 */
type ConfirmDone = (target: () => HTMLElement | null | undefined) => void

/**
 * Confirming a change to the key, as RemovePinDialog confirms removing the PIN: the
 * inverse button beside "Cancelar", the request's failure above them, with focus
 * back on that button. On success the button that opened it is gone (the view
 * changed underneath), so focus goes where the request says, after the confirmation
 * has left the page; cancelled, it goes back to that button.
 *
 * While the request runs there is no way out: "Cancelar" is disabled, "Cerrar" is
 * not shown, and Escape or a tap outside does nothing. Its answer, the key or the
 * failure, then always lands here, where the person is looking.
 */
function ConfirmDialog({
  open,
  onOpenChange,
  title,
  description,
  action,
  pending,
  failure,
  onConfirm,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  title: string
  description: string
  action: string
  pending: boolean
  failure: unknown
  onConfirm: (done: ConfirmDone) => void
}) {
  const returnFocus = useReturnFocus()
  const confirmRef = useRef<HTMLButtonElement>(null)
  // Where focus goes once the confirmation has left the page.
  const focusAfter = useRef<(() => HTMLElement | null | undefined) | null>(null)

  // The button was disabled while the request ran; a failure brings it back.
  useEffect(() => {
    if (failure != null) focusWhenReady(() => confirmRef.current)
  }, [failure])

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next && pending) return
        onOpenChange(next)
      }}
    >
      <DialogContent
        className="sm:max-w-104"
        showCloseButton={!pending}
        onOpenAutoFocus={() => {
          focusAfter.current = null
          returnFocus.onOpenAutoFocus()
        }}
        onCloseAutoFocus={(event) => {
          const target = focusAfter.current
          focusAfter.current = null
          if (!target) return returnFocus.onCloseAutoFocus(event)
          event.preventDefault()
          focusWhenReady(target)
        }}
      >
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>{description}</DialogDescription>
        </DialogHeader>

        {failure != null && (
          <Alert variant="attention">
            <CircleAlertIcon aria-hidden="true" />
            <AlertDescription className="text-foreground">
              {problemMessage(failure)}
            </AlertDescription>
          </Alert>
        )}

        <DialogFooter>
          <Button
            type="button"
            variant="secondary"
            onClick={() => onOpenChange(false)}
            disabled={pending}
          >
            Cancelar
          </Button>
          <Button
            ref={confirmRef}
            type="button"
            variant="destructive"
            disabled={pending}
            onClick={() =>
              onConfirm((target) => {
                focusAfter.current = target
                onOpenChange(false)
              })
            }
          >
            {pending && <Spinner />}
            {action}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

/** An iOS label as Atajos shows it, set apart so the steps can be scanned. */
function IosLabel({ children }: { children: ReactNode }) {
  return <span className="font-semibold text-foreground">{children}</span>
}

/**
 * "¿Cómo lo configuro?": the steps for iOS 27, where the Wallet trigger sits at the
 * top of the shortcut itself, then, folded, for iOS 17 to 26, where a separate
 * automation runs the shortcut. Only the labels Atajos shows in Spanish.
 */
function SetupSteps({ card }: { card: string }) {
  return (
    <details className="group rounded-2xl bg-muted px-4 py-1">
      <summary className="flex min-h-11 cursor-pointer list-none items-center justify-between gap-3 text-body font-semibold text-foreground [&::-webkit-details-marker]:hidden">
        ¿Cómo lo configuro?
        <ChevronDownIcon
          aria-hidden="true"
          className="size-5 shrink-0 text-muted-foreground transition-transform duration-180 ease-out group-open:rotate-180"
        />
      </summary>
      <div className="flex flex-col gap-3 pt-1 pb-3 text-small font-medium text-pretty text-muted-foreground">
        <h3 className="font-semibold text-foreground">En iOS 27</h3>
        <ol className="flex list-decimal flex-col gap-2 pl-5">
          <li>
            En <IosLabel>Atajos</IosLabel>, crea un atajo y agrega arriba el disparador{' '}
            <IosLabel>Wallet</IosLabel>. Elige {card}.
          </li>
          <li>
            Activa <IosLabel>Automatización</IosLabel> y apaga{' '}
            <IosLabel>Notificar</IosLabel>.
          </li>
          <li>
            Agrega <IosLabel>Obtener contenido de URL</IosLabel> con la Dirección. En{' '}
            <IosLabel>Método</IosLabel>, elige <IosLabel>POST</IosLabel>.
          </li>
          <li>
            En <IosLabel>Encabezados</IosLabel> agrega uno solo:{' '}
            <IosLabel>Authorization</IosLabel>, con tu clave como valor.
          </li>
          <li>
            En <IosLabel>Cuerpo de la solicitud</IosLabel> elige <IosLabel>JSON</IosLabel> y
            agrega <IosLabel>amount</IosLabel> con <IosLabel>Monto</IosLabel>,{' '}
            <IosLabel>merchant</IosLabel> con <IosLabel>Comercio</IosLabel> y{' '}
            <IosLabel>card</IosLabel> con <IosLabel>Tarjeta</IosLabel>, de la{' '}
            <IosLabel>Entrada del atajo</IosLabel>, y <IosLabel>date</IosLabel> con{' '}
            <IosLabel>Fecha actual</IosLabel>.
          </li>
          <li>
            Agrega <IosLabel>Obtener valor del diccionario</IosLabel> para{' '}
            <IosLabel>message</IosLabel> y, al final,{' '}
            <IosLabel>Mostrar notificación</IosLabel> con ese valor.
          </li>
        </ol>

        <details className="group/older">
          <summary className="-mx-2 flex min-h-11 cursor-pointer list-none items-center justify-between gap-3 rounded-full px-2 font-semibold text-foreground [&::-webkit-details-marker]:hidden">
            ¿Tu iPhone tiene iOS 17 a 26?
            <ChevronDownIcon
              aria-hidden="true"
              className="size-5 shrink-0 text-muted-foreground transition-transform duration-180 ease-out group-open/older:rotate-180"
            />
          </summary>
          <ol className="flex list-decimal flex-col gap-2 pt-1 pl-5">
            <li>
              En <IosLabel>Atajos</IosLabel>, crea un atajo con los pasos 3 a 6.
            </li>
            <li>
              En <IosLabel>Automatización</IosLabel>, toca{' '}
              <IosLabel>Nueva automatización</IosLabel> y elige <IosLabel>Wallet</IosLabel>{' '}
              (en iOS 17 y 18, <IosLabel>Transacción</IosLabel>).
            </li>
            <li>
              Elige {card} y <IosLabel>Ejecutar inmediatamente</IosLabel>. Apaga{' '}
              <IosLabel>Notificar al ejecutarse</IosLabel>.
            </li>
            <li>
              Agrega la acción <IosLabel>Ejecutar atajo</IosLabel> con tu atajo y la{' '}
              <IosLabel>Entrada del atajo</IosLabel>.
            </li>
          </ol>
        </details>

        <p>
          Para probarlo, ejecuta el atajo una vez a mano: debe decir «Listo: el atajo está
          conectado…».
        </p>
      </div>
    </details>
  )
}
