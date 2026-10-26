/**
 * <SecuritySection id? className? />
 *
 * Ajustes › Seguridad (the `#seguridad` target): the PIN and this phone.
 *
 * A PIN never stands on its own. SignInPage only offers the PIN pad on a phone this
 * section has trusted, so there are two steps: create the PIN (any device), then
 * turn it on for this phone (only on a phone; useIsMobile).
 *
 *   PIN            no PIN: the form to create one, open.
 *                  a PIN: "Cambiar PIN" opens the same form; "Quitar PIN" asks for
 *                  the password in a dialog (it also switches the PIN off on every
 *                  phone that had it).
 *   Este teléfono  on a phone: "Activar" / "Desactivar". Elsewhere: a line saying
 *                  the PIN only works on a phone.
 *
 * The card renders at once (with a skeleton inside) so the shell can scroll to
 * #seguridad on the first render; SettingsPage scrolls again once the data lands.
 *
 * Focus never falls to <body> when a request swaps what is on screen (focusWhenReady):
 * an invalid form goes to its first field to fix, "Cambiar PIN" opens on the
 * password, Cancelar and a saved PIN come back to "Cambiar PIN", a removed PIN to
 * the PIN row's title, and Activar / Desactivar stay on their button.
 */
import { CircleAlertIcon, KeyRoundIcon, SmartphoneIcon } from 'lucide-react'
import { useEffect, useId, useRef, useState } from 'react'
import { toast } from 'sonner'

import { Notice, SectionCard } from '@/components/kit'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '@/components/ui/dialog'
import { Field, FieldDescription, FieldError, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import { Spinner } from '@/components/ui/spinner'
import {
  useDisableDeviceForPin,
  useEnableDeviceForPin,
  useRemovePin,
  useSecurityStatus,
  useSetPin,
} from '@/lib/queries'
import { useIsMobile } from '@/lib/useIsMobile'
import { isClean, pinProblem, type Errors } from '@/lib/validate'
import { focusWhenReady } from './focus'
import { fieldProblem, problemMessage } from './problem'
import { SettingsRow } from './SettingsRow'

const byId = (id: string) => () => document.getElementById(id)

export function SecuritySection({ id, className }: { id?: string; className?: string }) {
  const status = useSecurityStatus()
  const [changing, setChanging] = useState(false)
  const ids = useId()
  const pinTitleId = `${ids}-pin`
  const changeId = `${ids}-cambiar`

  const hasPin = status.data?.has_pin ?? false
  const deviceEnabled = status.data?.device_enabled ?? false
  const formOpen = status.isSuccess && (!hasPin || changing)

  return (
    <SectionCard id={id} title="Seguridad" className={className}>
      <p className="text-body text-pretty text-muted-foreground">
        Con un PIN de 6 dígitos entras desde tu teléfono sin escribir tu correo ni tu
        contraseña.
      </p>

      {status.isPending ? (
        <div className="mt-6 flex flex-col gap-6" aria-hidden="true">
          <RowSkeleton />
          <RowSkeleton />
        </div>
      ) : status.isError ? (
        // The kit's Notice drops "Reintentar" under the text on a phone, where an
        // Alert's side column squeezed the sentence to two words a line.
        <Notice
          className="mt-6"
          title="No pudimos cargar tu PIN"
          description={problemMessage(status.error)}
          action={{ label: 'Reintentar', onClick: () => void status.refetch() }}
        />
      ) : (
        <div className="mt-6 flex flex-col gap-6">
          <SettingsRow
            icon={KeyRoundIcon}
            title="PIN"
            titleId={pinTitleId}
            description={hasPin ? 'Ya tienes un PIN.' : 'Todavía no tienes un PIN.'}
            actions={
              hasPin && !changing ? (
                <>
                  <Button
                    id={changeId}
                    variant="secondary"
                    onClick={() => setChanging(true)}
                  >
                    Cambiar PIN
                  </Button>
                  <RemovePinDialog onRemoved={() => focusWhenReady(byId(pinTitleId))} />
                </>
              ) : undefined
            }
          />

          {formOpen && (
            <PinForm
              hasPin={hasPin}
              autoFocus={changing}
              onDone={() => {
                setChanging(false)
                focusWhenReady(byId(changeId))
              }}
              onCancel={
                hasPin
                  ? () => {
                      setChanging(false)
                      focusWhenReady(byId(changeId))
                    }
                  : undefined
              }
            />
          )}

          <DeviceRow hasPin={hasPin} enabled={deviceEnabled} quiet={formOpen} />
        </div>
      )}
    </SectionCard>
  )
}

function RowSkeleton() {
  return (
    <div className="flex items-center gap-3">
      <Skeleton className="size-10 shrink-0 rounded-full" />
      <div className="flex flex-1 flex-col gap-2">
        <Skeleton className="h-4 w-24" />
        <Skeleton className="h-3.5 w-48 max-w-full" />
      </div>
    </div>
  )
}

type PinField = 'current_password' | 'pin' | 'confirm_pin'

/**
 * Create a PIN, or replace the one there is. Both need the account's password.
 * `autoFocus` puts focus on the password when the form opens from "Cambiar PIN".
 */
function PinForm({
  hasPin,
  autoFocus = false,
  onDone,
  onCancel,
}: {
  hasPin: boolean
  autoFocus?: boolean
  onDone: () => void
  onCancel?: () => void
}) {
  const setPin = useSetPin()
  const ids = useId()
  const submitRef = useRef<HTMLButtonElement>(null)
  const [password, setPassword] = useState('')
  const [pin, setPinValue] = useState('')
  const [confirm, setConfirm] = useState('')
  const [errors, setErrors] = useState<Errors<PinField>>({})
  const [failure, setFailure] = useState<string | null>(null)

  const passwordId = `${ids}-contrasena`
  const pinId = `${ids}-pin`
  const confirmId = `${ids}-confirmar`
  const fieldIds: Record<PinField, string> = {
    current_password: passwordId,
    pin: pinId,
    confirm_pin: confirmId,
  }

  useEffect(() => {
    if (autoFocus) document.getElementById(passwordId)?.focus()
  }, [autoFocus, passwordId])

  const clear = (field: PinField) =>
    setErrors((previous) =>
      previous[field] ? { ...previous, [field]: undefined } : previous,
    )

  /** Focus the first field with a problem, in the order they are on screen. */
  const focusFirst = (found: Errors<PinField>) => {
    const first = (Object.keys(fieldIds) as PinField[]).find((field) => found[field])
    if (first) focusWhenReady(byId(fieldIds[first]))
    return Boolean(first)
  }

  async function submit(event: React.FormEvent) {
    event.preventDefault()
    if (setPin.isPending) return
    const found: Errors<PinField> = {
      current_password: password ? undefined : 'Escribe tu contraseña.',
      pin: pinProblem(pin),
      confirm_pin: confirm === pin ? undefined : 'Los dos PIN no son iguales.',
    }
    setErrors(found)
    setFailure(null)
    if (!isClean(found)) {
      focusFirst(found)
      return
    }

    try {
      await setPin.mutateAsync({ currentPassword: password, pin })
      toast(hasPin ? 'Listo, cambiaste tu PIN.' : 'Listo, ya tienes PIN.')
      setPassword('')
      setPinValue('')
      setConfirm('')
      onDone()
    } catch (error) {
      const fields: Errors<PinField> = {
        current_password: fieldProblem(error, 'current_password'),
        pin: fieldProblem(error, 'pin'),
      }
      setErrors(fields)
      if (!focusFirst(fields)) {
        setFailure(problemMessage(error))
        focusWhenReady(() => submitRef.current)
      }
    }
  }

  return (
    <form
      onSubmit={submit}
      noValidate
      aria-label={hasPin ? 'Cambiar PIN' : 'Crear PIN'}
      className="@container flex flex-col gap-5"
    >
      {failure && (
        <Alert variant="attention">
          <CircleAlertIcon aria-hidden="true" />
          <AlertTitle>No pudimos guardar el PIN</AlertTitle>
          <AlertDescription>{failure}</AlertDescription>
        </Alert>
      )}

      <Field data-invalid={errors.current_password ? true : undefined}>
        <FieldLabel htmlFor={passwordId}>Tu contraseña</FieldLabel>
        <Input
          id={passwordId}
          type="password"
          autoComplete="current-password"
          value={password}
          aria-invalid={errors.current_password ? true : undefined}
          aria-describedby={`${passwordId}-ayuda`}
          onChange={(event) => {
            setPassword(event.target.value)
            clear('current_password')
          }}
        />
        {errors.current_password ? (
          <FieldError id={`${passwordId}-ayuda`}>{errors.current_password}</FieldError>
        ) : (
          <FieldDescription id={`${passwordId}-ayuda`}>
            La de siempre, para confirmar que eres tú.
          </FieldDescription>
        )}
      </Field>

      <div className="grid gap-5 @md:grid-cols-2 @md:gap-4">
        <Field data-invalid={errors.pin ? true : undefined}>
          <FieldLabel htmlFor={pinId}>{hasPin ? 'PIN nuevo' : 'Tu PIN'}</FieldLabel>
          <Input
            id={pinId}
            type="password"
            inputMode="numeric"
            pattern="[0-9]*"
            autoComplete="off"
            maxLength={6}
            placeholder="6 dígitos"
            value={pin}
            aria-invalid={errors.pin ? true : undefined}
            onChange={(event) => {
              setPinValue(event.target.value.replace(/\D/g, '').slice(0, 6))
              clear('pin')
            }}
          />
          {errors.pin && <FieldError>{errors.pin}</FieldError>}
        </Field>

        <Field data-invalid={errors.confirm_pin ? true : undefined}>
          <FieldLabel htmlFor={confirmId}>Repite el PIN</FieldLabel>
          <Input
            id={confirmId}
            type="password"
            inputMode="numeric"
            pattern="[0-9]*"
            autoComplete="off"
            maxLength={6}
            placeholder="6 dígitos"
            value={confirm}
            aria-invalid={errors.confirm_pin ? true : undefined}
            onChange={(event) => {
              setConfirm(event.target.value.replace(/\D/g, '').slice(0, 6))
              clear('confirm_pin')
            }}
          />
          {errors.confirm_pin && <FieldError>{errors.confirm_pin}</FieldError>}
        </Field>
      </div>

      <div className="flex flex-col gap-2 @md:flex-row">
        <Button
          ref={submitRef}
          type="submit"
          disabled={setPin.isPending}
          className="max-md:h-12 max-md:w-full"
        >
          {setPin.isPending && <Spinner />}
          {hasPin ? 'Guardar PIN' : 'Crear PIN'}
        </Button>
        {onCancel && (
          <Button
            type="button"
            variant="secondary"
            onClick={onCancel}
            disabled={setPin.isPending}
            className="max-md:h-12 max-md:w-full"
          >
            Cancelar
          </Button>
        )}
      </div>
    </form>
  )
}

/**
 * Removing the PIN also switches it off on every phone that had it. Once it is gone
 * this dialog and its trigger leave the page with it (the row has nothing to remove
 * any more), so the toast and the focus are handled from the awaited request rather
 * than from mutate callbacks, which do not run for an unmounted caller.
 */
function RemovePinDialog({ onRemoved }: { onRemoved: () => void }) {
  const removePin = useRemovePin()
  const passwordId = useId()
  const [open, setOpen] = useState(false)
  const [password, setPassword] = useState('')
  const [missing, setMissing] = useState(false)
  const removed = useRef(false)

  const failure = removePin.error
  const message = missing
    ? 'Escribe tu contraseña.'
    : failure
      ? (fieldProblem(failure, 'current_password') ?? problemMessage(failure))
      : null

  async function submit(event: React.FormEvent) {
    event.preventDefault()
    if (removePin.isPending) return
    if (!password) {
      setMissing(true)
      focusWhenReady(byId(passwordId))
      return
    }
    try {
      await removePin.mutateAsync(password)
      removed.current = true
      setOpen(false)
      setPassword('')
      toast('Quitaste tu PIN.')
      onRemoved()
    } catch {
      // The message shows under the field (removePin.error); put focus back there.
      focusWhenReady(byId(passwordId))
    }
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        setOpen(next)
        if (!next) {
          setPassword('')
          setMissing(false)
          removePin.reset()
        }
      }}
    >
      <DialogTrigger asChild>
        <Button variant="plain">Quitar PIN</Button>
      </DialogTrigger>

      <DialogContent
        onCloseAutoFocus={(event) => {
          // After a removal the trigger is gone; onRemoved has put focus on the row.
          if (removed.current) {
            removed.current = false
            event.preventDefault()
          }
        }}
      >
        <form
          onSubmit={(event) => void submit(event)}
          noValidate
          className="flex flex-col gap-5"
        >
          <DialogHeader>
            <DialogTitle>¿Quitar tu PIN?</DialogTitle>
            <DialogDescription>
              Dejas de entrar con PIN en todos tus teléfonos. Puedes crear otro cuando
              quieras.
            </DialogDescription>
          </DialogHeader>

          <Field data-invalid={message ? true : undefined}>
            <FieldLabel htmlFor={passwordId}>Tu contraseña</FieldLabel>
            <Input
              id={passwordId}
              type="password"
              autoComplete="current-password"
              value={password}
              aria-invalid={message ? true : undefined}
              disabled={removePin.isPending}
              onChange={(event) => {
                setPassword(event.target.value)
                setMissing(false)
                if (removePin.isError) removePin.reset()
              }}
            />
            {message && <FieldError>{message}</FieldError>}
          </Field>

          <DialogFooter className="mt-0">
            <Button
              type="button"
              variant="secondary"
              onClick={() => setOpen(false)}
              disabled={removePin.isPending}
            >
              Cancelar
            </Button>
            <Button type="submit" variant="destructive" disabled={removePin.isPending}>
              {removePin.isPending && <Spinner />}
              Quitar PIN
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

/**
 * Trusting this phone for the PIN. Only offered on a phone: the PIN pad only ever
 * shows on one (SignInPage), so trusting a laptop would switch on something its
 * sign-in screen never offers.
 */
function DeviceRow({
  hasPin,
  enabled,
  quiet,
}: {
  hasPin: boolean
  enabled: boolean
  /** The PIN form is open: keep this row's button secondary, one primary per view. */
  quiet: boolean
}) {
  const isMobile = useIsMobile()
  const enable = useEnableDeviceForPin()
  const disable = useDisableDeviceForPin()
  const [failure, setFailure] = useState<string | null>(null)
  const pending = enable.isPending || disable.isPending
  const buttonRef = useRef<HTMLButtonElement>(null)

  if (!isMobile) {
    return (
      <SettingsRow
        icon={SmartphoneIcon}
        title="Entrar con PIN"
        description="Funciona solo en tu teléfono. Abre Ajustes desde ahí para activarlo."
      />
    )
  }

  async function toggle() {
    if (pending) return
    setFailure(null)
    try {
      if (enabled) {
        await disable.mutateAsync()
        toast('Este teléfono ya no entra con PIN.')
      } else {
        await enable.mutateAsync()
        toast('Listo, este teléfono entra con tu PIN.')
      }
    } catch (error) {
      setFailure(problemMessage(error))
    }
    // The button was disabled while the request ran, which drops focus; it is the
    // same button afterwards (Activar ↔ Desactivar), so put focus back on it.
    focusWhenReady(() => buttonRef.current)
  }

  const description = !hasPin
    ? 'Crea tu PIN y luego actívalo aquí.'
    : enabled
      ? 'Ya entras con tu PIN desde aquí.'
      : 'Actívalo y la próxima vez entras solo con tu PIN.'

  return (
    <div className="flex flex-col gap-3">
      <SettingsRow
        icon={SmartphoneIcon}
        title="Este teléfono"
        description={description}
        actions={
          hasPin ? (
            <Button
              ref={buttonRef}
              variant={enabled || quiet ? 'secondary' : 'default'}
              disabled={pending}
              onClick={() => void toggle()}
            >
              {pending && <Spinner />}
              {enabled ? 'Desactivar' : 'Activar en este teléfono'}
            </Button>
          ) : undefined
        }
      />
      {failure && (
        <p role="alert" className="pl-13 text-small font-medium text-destructive">
          {failure}
        </p>
      )}
    </div>
  )
}
