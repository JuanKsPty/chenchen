import { CheckIcon, CircleAlertIcon } from 'lucide-react'
import { useEffect, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'

import { ApiError } from '@/api/client'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Field, FieldDescription, FieldError, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Spinner } from '@/components/ui/spinner'
import { HOME_PATH } from '@/lib/nav'
import { useSession } from '@/lib/session'
import { useDocumentTitle } from '@/lib/title'
import { useIsMobile } from '@/lib/useIsMobile'
import { cn } from '@/lib/utils'
import {
  emailProblem,
  fullNameProblem,
  isClean,
  passwordProblem,
  type Errors,
} from '@/lib/validate'
import { AuthLayout, Reference } from './AuthLayout'
import { preloadSignedInApp, whenIdle } from './lazy'

type FieldName = 'full_name' | 'email' | 'password'

const FIELD_ID: Record<FieldName, string> = {
  full_name: 'nombre',
  email: 'correo',
  password: 'contrasena',
}

/** The first field with a problem gets the focus, so the fix starts there. */
function focusFirst(errors: Errors<FieldName>) {
  const first = (['full_name', 'email', 'password'] as const).find((f) => errors[f])
  if (first) document.getElementById(FIELD_ID[first])?.focus()
}

const PASSWORD_RULE = 'Al menos 8 caracteres, con una letra y un número.'

/** Registro: name, e-mail and password, then straight to Inicio. */
export function SignUpPage() {
  useDocumentTitle('Crear cuenta')
  // Somebody on this form is about to be inside: fetch the shell while they type.
  useEffect(() => whenIdle(() => preloadSignedInApp()), [])

  const { signUp } = useSession()
  const navigate = useNavigate()
  const isMobile = useIsMobile()

  const [fullName, setFullName] = useState('')
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [errors, setErrors] = useState<Errors<FieldName>>({})
  const [failure, setFailure] = useState<ApiError | null>(null)
  const [submitting, setSubmitting] = useState(false)

  // Checked on every keystroke, but it only ever confirms; the rule itself stays
  // visible until then, and a real problem waits for the submit.
  const passwordOk = password !== '' && !passwordProblem(password)

  async function handleSubmit(event: React.FormEvent) {
    event.preventDefault()
    if (submitting) return

    // An empty field asks for what is missing, in the same words as Entrar.
    const found: Errors<FieldName> = {
      full_name: fullName.trim() ? fullNameProblem(fullName) : 'Escribe tu nombre.',
      email: email.trim() ? emailProblem(email) : 'Escribe tu correo.',
      password: password ? passwordProblem(password) : 'Escribe una contraseña.',
    }
    setErrors(found)
    if (!isClean(found)) {
      focusFirst(found)
      return
    }

    setSubmitting(true)
    setFailure(null)
    try {
      await signUp({ full_name: fullName.trim(), email: email.trim(), password })
      navigate(HOME_PATH, { replace: true })
    } catch (error) {
      if (error instanceof ApiError) {
        setFailure(error)
        // "Este correo ya está registrado." lands on the e-mail field, and the
        // focus goes there with it.
        if (error.fields) {
          setErrors(error.fields as Errors<FieldName>)
          focusFirst(error.fields as Errors<FieldName>)
        }
      } else {
        setFailure(
          new ApiError(0, {
            code: 'network',
            message: 'No pudimos conectar. Revisa tu conexión e intenta otra vez.',
          }),
        )
      }
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <AuthLayout
      title="Crea tu cuenta"
      subtitle="Un minuto y subes tu primer estado de cuenta."
      footer={
        <>
          <span>¿Ya tienes cuenta?</span>
          <Button asChild variant="ghost">
            <Link to="/entrar">Entrar</Link>
          </Button>
        </>
      }
    >
      <form onSubmit={handleSubmit} noValidate className="flex flex-col gap-5">
        {/* A field-level failure already shows on its field; this is for what the
            form cannot pin on one input. */}
        {failure && !failure.fields && (
          <Alert variant="attention">
            <CircleAlertIcon aria-hidden="true" />
            <AlertTitle>No pudimos crear tu cuenta</AlertTitle>
            <AlertDescription>
              {failure.status >= 500
                ? 'Algo falló de nuestro lado. Intenta otra vez en un momento.'
                : failure.message}
              <Reference failure={failure} />
            </AlertDescription>
          </Alert>
        )}

        <Field data-invalid={errors.full_name ? true : undefined}>
          <FieldLabel htmlFor="nombre">Tu nombre</FieldLabel>
          <Input
            id="nombre"
            autoComplete="name"
            // As on Entrar: a phone's keyboard waits for a tap.
            autoFocus={!isMobile}
            value={fullName}
            aria-invalid={errors.full_name ? true : undefined}
            aria-describedby={errors.full_name ? 'nombre-error' : undefined}
            onChange={(event) => {
              setFullName(event.target.value)
              if (errors.full_name) setErrors((p) => ({ ...p, full_name: undefined }))
            }}
            placeholder="Ana Pérez"
          />
          {errors.full_name && (
            <FieldError id="nombre-error">{errors.full_name}</FieldError>
          )}
        </Field>

        <Field data-invalid={errors.email ? true : undefined}>
          <FieldLabel htmlFor="correo">Correo</FieldLabel>
          <Input
            id="correo"
            type="email"
            inputMode="email"
            autoComplete="username"
            value={email}
            aria-invalid={errors.email ? true : undefined}
            aria-describedby={errors.email ? 'correo-error' : undefined}
            onChange={(event) => {
              setEmail(event.target.value)
              if (errors.email) setErrors((p) => ({ ...p, email: undefined }))
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
            autoComplete="new-password"
            value={password}
            aria-invalid={errors.password ? true : undefined}
            aria-describedby={errors.password ? 'contrasena-error' : 'contrasena-ayuda'}
            onChange={(event) => {
              setPassword(event.target.value)
              if (errors.password) setErrors((p) => ({ ...p, password: undefined }))
            }}
          />
          {errors.password ? (
            <FieldError id="contrasena-error">{errors.password}</FieldError>
          ) : (
            // One element whose text changes, so the live region is announced.
            <FieldDescription
              id="contrasena-ayuda"
              aria-live="polite"
              className={cn(passwordOk && 'flex items-center gap-1.5 text-positive')}
            >
              {passwordOk ? (
                <>
                  <CheckIcon aria-hidden="true" className="size-4" />
                  Cumple los requisitos
                </>
              ) : (
                PASSWORD_RULE
              )}
            </FieldDescription>
          )}
        </Field>

        {/* aria-disabled, not disabled: a disabled button drops the keyboard focus
            to <body> while the request runs. */}
        <Button
          type="submit"
          size="lg"
          aria-disabled={submitting || undefined}
          className="mt-3 w-full aria-disabled:cursor-wait"
        >
          {submitting && <Spinner aria-hidden="true" />}
          {submitting ? 'Creando tu cuenta…' : 'Crear cuenta'}
        </Button>
      </form>
    </AuthLayout>
  )
}
