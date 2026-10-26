import { CircleAlertIcon } from 'lucide-react'
import { useEffect, useState } from 'react'
import { Link, useLocation, useNavigate } from 'react-router-dom'

import { ApiError } from '@/api/client'
import { fetchDeviceStatus } from '@/api/endpoints'
import type { DeviceStatus } from '@/api/types'
import { PinPad } from '@/components/auth/PinPad'
import { Isotipo } from '@/components/brand'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Field, FieldError, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Spinner } from '@/components/ui/spinner'
import { useSession } from '@/lib/session'
import { useDocumentTitle } from '@/lib/title'
import { useIsMobile } from '@/lib/useIsMobile'
import { emailProblem, isClean, type Errors } from '@/lib/validate'
import { AuthLayout, Reference } from './AuthLayout'
import { preloadSignedInApp, whenIdle } from './lazy'

type FieldName = 'email' | 'password'

/** Why the e-mail form is showing instead of the PIN pad this device offers. */
type EmailReason = 'pin-help' | 'device-lost'

const SUBTITLE: Record<EmailReason | 'default', string> = {
  default: 'Entra con tu correo y contraseña.',
  // There is no PIN reset: the way back is the password, then Ajustes.
  'pin-help': 'Entra con tu correo y contraseña. Luego cambias el PIN en Ajustes.',
  'device-lost':
    'El PIN ya no está activo en este teléfono. Entra con tu correo y contraseña.',
}

const GENERIC_FAILURE =
  'Algo falló de nuestro lado. Tus datos están bien; intenta otra vez en un momento.'
const NETWORK_FAILURE = 'No pudimos conectar. Revisa tu conexión e intenta otra vez.'

/**
 * Entrar.
 *
 * On a phone that was trusted for a PIN (Ajustes), the PIN pad (spec pinPad,
 * artboard C-Entrar-Mobile): the isotipo, "Hola de nuevo, {nombre}", six dots and a
 * drawn keypad of real buttons. It submits on the sixth digit. Everywhere else, and
 * whenever the PIN cannot help ("¿Olvidaste tu PIN?", "No soy Juan", a device the
 * server stopped trusting), the e-mail form.
 */
export function SignInPage() {
  useDocumentTitle('Entrar')
  // Somebody on this form is about to be inside: fetch the shell while they type.
  useEffect(() => whenIdle(() => preloadSignedInApp()), [])

  const { signIn, signInWithPin } = useSession()
  const navigate = useNavigate()
  const location = useLocation()
  const isMobile = useIsMobile()

  // Where the visitor was headed before the session ended, so an expiry returns
  // them to that page rather than to Inicio.
  const returnTo = (location.state as { from?: string } | null)?.from ?? '/'

  // The PIN is a phone shortcut on purpose: a computer keeps typing a password. So a
  // desktop never asks whether it is trusted, and its form renders immediately
  // instead of behind a wait nobody there needs.
  const [device, setDevice] = useState<DeviceStatus | 'checking'>(() =>
    isMobile ? 'checking' : { trusted: false },
  )
  const [emailReason, setEmailReason] = useState<EmailReason | null>(null)

  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [errors, setErrors] = useState<Errors<FieldName>>({})
  const [failure, setFailure] = useState<ApiError | null>(null)
  const [submitting, setSubmitting] = useState(false)

  useEffect(() => {
    if (!isMobile) return
    let cancelled = false
    void (async () => {
      try {
        const status = await fetchDeviceStatus()
        if (!cancelled) setDevice(status)
      } catch {
        // A device the server could not vouch for is treated as untrusted, not
        // retried: the e-mail form always works, and trusting it blindly would not.
        if (!cancelled) setDevice({ trusted: false })
      }
    })()
    return () => {
      cancelled = true
    }
  }, [isMobile])

  async function handlePin(pin: string): Promise<string | null> {
    try {
      await signInWithPin(pin)
      navigate(returnTo, { replace: true })
      return null
    } catch (error) {
      if (!(error instanceof ApiError)) return NETWORK_FAILURE
      if (error.code === 'device_not_trusted') {
        // The device itself is done (too many wrong PINs, or it was turned off in
        // Ajustes meanwhile). A PIN pad that can never work again is worse than
        // the form that always does.
        setDevice({ trusted: false })
        setEmailReason('device-lost')
        return null
      }
      if (error.code === 'pin_incorrect') return 'Ese PIN no es. Intenta otra vez.'
      if (error.status === 429) return 'Demasiados intentos. Espera un momento.'
      if (error.status >= 500) return 'Algo falló de nuestro lado. Intenta otra vez.'
      return error.message
    }
  }

  async function handleSubmit(event: React.FormEvent) {
    event.preventDefault()
    if (submitting) return

    const found: Errors<FieldName> = {
      email: email.trim() ? emailProblem(email) : 'Escribe tu correo.',
      // Not the full password policy: on a sign-in only emptiness matters. Telling
      // somebody their existing password is too short would be nonsense.
      password: password ? undefined : 'Escribe tu contraseña.',
    }
    setErrors(found)
    if (!isClean(found)) {
      document.getElementById(found.email ? 'correo' : 'contrasena')?.focus()
      return
    }

    setSubmitting(true)
    setFailure(null)
    try {
      await signIn(email.trim(), password)
      navigate(returnTo, { replace: true })
    } catch (error) {
      if (error instanceof ApiError) {
        setFailure(error)
        // Field-level messages when the server sent them, so a rejected e-mail
        // lands on the e-mail field, and the focus goes there with it.
        if (error.fields) {
          setErrors(error.fields as Errors<FieldName>)
          const first = error.fields.email
            ? 'correo'
            : error.fields.password && 'contrasena'
          if (first) document.getElementById(first)?.focus()
        }
      } else {
        setFailure(new ApiError(0, { code: 'network', message: NETWORK_FAILURE }))
      }
    } finally {
      setSubmitting(false)
    }
  }

  if (device === 'checking') {
    return (
      <main className="flex min-h-dvh flex-col items-center justify-center gap-4 bg-background px-4">
        <Isotipo size={56} title={null} />
        <p role="status" className="text-small font-medium text-muted-foreground">
          Un momento…
        </p>
      </main>
    )
  }

  const firstName = device.full_name?.trim().split(/\s+/)[0] ?? ''

  if (isMobile && device.trusted && emailReason === null) {
    // The pad is the first screen of the installed app, so it clears the notch
    // and the home indicator (index.html sets viewport-fit=cover). On a short
    // viewport (an SE, or Safari with its bars showing, about 664px on a 390x844
    // phone) the spacing tightens so "¿Olvidaste tu PIN?" stays on screen.
    return (
      <main className="flex min-h-dvh flex-col items-center bg-background px-4 pt-[calc(3rem+env(safe-area-inset-top))] pb-[calc(1rem+env(safe-area-inset-bottom))] [@media(max-height:45rem)]:pt-[calc(1.5rem+env(safe-area-inset-top))]">
        <Isotipo size={56} />
        <h1 className="mt-4 text-center font-display text-title font-bold text-balance">
          {firstName ? `Hola de nuevo, ${firstName}` : 'Hola de nuevo'}
        </h1>
        <p className="mt-2 text-body text-muted-foreground">Escribe tu PIN</p>

        <PinPad className="mt-8 [@media(max-height:45rem)]:mt-6" onComplete={handlePin} />

        <div className="mt-auto flex flex-wrap justify-center gap-2 pt-6 [@media(max-height:45rem)]:pt-2">
          <Button variant="ghost" onClick={() => setEmailReason('pin-help')}>
            ¿Olvidaste tu PIN?
          </Button>
          {firstName && (
            <Button variant="ghost" onClick={() => setEmailReason('pin-help')}>
              No soy {firstName}
            </Button>
          )}
        </div>
      </main>
    )
  }

  const canUsePin = isMobile && device.trusted

  return (
    <AuthLayout
      title="Hola de nuevo"
      subtitle={SUBTITLE[emailReason ?? 'default']}
      footer={
        <>
          <span>¿No tienes cuenta?</span>
          <Button asChild variant="ghost">
            <Link to="/registro">Crear cuenta</Link>
          </Button>
        </>
      }
    >
      <form onSubmit={handleSubmit} noValidate className="flex flex-col gap-5">
        {failure && !failure.fields && (
          <Alert variant="attention">
            <CircleAlertIcon aria-hidden="true" />
            <AlertTitle>
              {failure.status === 429 ? 'Demasiados intentos' : 'No pudimos entrar'}
            </AlertTitle>
            <AlertDescription>
              {failure.status >= 500 ? GENERIC_FAILURE : failure.message}
              <Reference failure={failure} />
            </AlertDescription>
          </Alert>
        )}

        <Field data-invalid={errors.email ? true : undefined}>
          <FieldLabel htmlFor="correo">Correo</FieldLabel>
          <Input
            id="correo"
            type="email"
            inputMode="email"
            autoComplete="username"
            // On a phone the keyboard waits for a tap, unless the visitor just
            // asked for this form from the PIN pad.
            autoFocus={!isMobile || emailReason !== null}
            value={email}
            aria-invalid={errors.email ? true : undefined}
            aria-describedby={errors.email ? 'correo-error' : undefined}
            onChange={(event) => {
              setEmail(event.target.value)
              if (errors.email) setErrors((previous) => ({ ...previous, email: undefined }))
            }}
            placeholder="tu@correo.com"
          />
          {errors.email && <FieldError id="correo-error">{errors.email}</FieldError>}
        </Field>

        <Field data-invalid={errors.password ? true : undefined}>
          <FieldLabel htmlFor="contrasena">Contraseña</FieldLabel>
          <Input
            id="contrasena"
            type="password"
            autoComplete="current-password"
            value={password}
            aria-invalid={errors.password ? true : undefined}
            aria-describedby={errors.password ? 'contrasena-error' : undefined}
            onChange={(event) => {
              setPassword(event.target.value)
              if (errors.password)
                setErrors((previous) => ({ ...previous, password: undefined }))
            }}
          />
          {errors.password && (
            <FieldError id="contrasena-error">{errors.password}</FieldError>
          )}
        </Field>

        <div className="mt-3 flex flex-col gap-2">
          {/* aria-disabled, not disabled: a disabled button drops the keyboard
              focus to <body> while the request runs, so a failed attempt would
              leave the visitor nowhere. */}
          <Button
            type="submit"
            size="lg"
            aria-disabled={submitting || undefined}
            className="w-full aria-disabled:cursor-wait"
          >
            {submitting && <Spinner aria-hidden="true" />}
            {submitting ? 'Entrando…' : 'Entrar'}
          </Button>
          {canUsePin && (
            <Button
              type="button"
              variant="ghost"
              className="w-full"
              onClick={() => {
                setFailure(null)
                setEmailReason(null)
              }}
            >
              Usar mi PIN
            </Button>
          )}
        </div>
      </form>
    </AuthLayout>
  )
}
