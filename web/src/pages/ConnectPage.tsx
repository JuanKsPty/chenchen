/**
 * Conectar (/conectar): where Claude's sign-in sends the owner to say yes or no
 * (ADR-0004). Outside the shell, framed like Entrar (AuthLayout): it is a step in
 * somebody else's flow, and the way out of it is back to Claude, not into the app.
 *
 *   ?req=<id>    the request. Opening it (GET /api/oauth/requests/:id) is what lets
 *                this owner answer it. "Claude quiere conectarse a chenchén" (or
 *                "Claude Code"), where the answer goes ("Al contestar vuelves a
 *                claude.ai."), and what it can do:
 *                  "Ver tus cuentas, movimientos y gastos", always;
 *                  "Anotar compras, ordenar tus movimientos y subir los archivos de
 *                  tu banco", a switch that starts as the client asked (on when its
 *                  scopes include write) and the owner turns either way.
 *                [Permitir], the one primary, approves with that switch's answer;
 *                [Cancelar], a ghost, says no. Either way the API answers with
 *                Claude's address, and the browser goes there.
 *   ?error=<c>   the API would not even start: invalid_client (not Claude),
 *                invalid_redirect (an address that is not Claude's) or rate_limited
 *                (too many tries from here). Said in Spanish, with nothing to answer.
 *                Shown signed out too (App): it needs no session to read.
 *
 * A request that expired, was answered already or is someone else's is a 404, and
 * so is one answered after it expired on screen: a calm "ya no está vigente" and the
 * way home. Signed out, App sends the visit through Entrar first and back here with
 * its query.
 */
import { CircleAlertIcon, EyeIcon, PencilLineIcon } from 'lucide-react'
import { useId, useState, type ReactNode } from 'react'
import { Link, useSearchParams } from 'react-router-dom'

import { isNotFound } from '@/api/client'
import { Isotipo } from '@/components/brand'
import { Tile } from '@/components/kit'
import { problemMessage } from '@/components/settings/problem'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Spinner } from '@/components/ui/spinner'
import { Switch } from '@/components/ui/switch'
import { HOME_PATH } from '@/lib/nav'
import { useAnswerConnectRequest, useConnectRequest } from '@/lib/queries'
import { useSession } from '@/lib/session'
import { useDocumentTitle } from '@/lib/title'
import { AuthLayout } from './AuthLayout'

/** What each refusal the API sends here means, for the person in the browser. */
const REFUSALS: Record<string, { title: string; text: string }> = {
  invalid_client: {
    title: 'Esta app no puede conectarse',
    text: 'Solo Claude puede conectarse a chenchén, y la app que lo intentó no es Claude. No se conectó nada.',
  },
  invalid_redirect: {
    title: 'No seguimos con esta conexión',
    text: 'Te íbamos a devolver a una dirección que no es de Claude, así que paramos aquí. No se conectó nada. Vuelve a intentarlo desde Claude.',
  },
  rate_limited: {
    title: 'Demasiados intentos',
    text: 'Espera un minuto y vuelve a conectar chenchén desde Claude.',
  },
}

const REFUSED = {
  title: 'No pudimos conectar a Claude',
  text: 'No se conectó nada. Vuelve a intentarlo desde Claude.',
}

const EXPIRED = {
  title: 'Esta solicitud ya no está vigente',
  text: 'Una solicitud de Claude dura 10 minutos y se contesta una sola vez. Para conectarlo, vuelve a intentarlo desde Claude.',
}

export function ConnectPage() {
  useDocumentTitle('Conectar Claude')
  const [params] = useSearchParams()
  const id = params.get('req') ?? ''
  const refusal = params.get('error')

  if (refusal && !id) {
    const { title, text } = REFUSALS[refusal] ?? REFUSED
    return <Explained title={title} text={text} />
  }
  if (!id) return <Explained title={EXPIRED.title} text={EXPIRED.text} />
  // Keyed by the request, so another ?req= starts afresh.
  return <Consent key={id} id={id} />
}

function Consent({ id }: { id: string }) {
  const request = useConnectRequest(id)
  const answer = useAnswerConnectRequest()
  // Unset until the owner touches the switch: until then it says what the client asked
  // for, from the request read once. Granting write to a client that asked only to read
  // is the owner's call (the API allows it), so the switch turns either way.
  const [chosen, setWrite] = useState<boolean>()
  const write = chosen ?? request.data?.scopes.includes('write') ?? false
  // The API answered with an address the browser cannot go to. Never expected.
  const [stuck, setStuck] = useState(false)
  const ids = useId()
  const writeTitleId = `${ids}-anotar`
  const writeHintId = `${ids}-anotar-ayuda`

  if (request.isPending) {
    return (
      <main className="flex min-h-dvh flex-col items-center justify-center gap-4 bg-background px-4">
        <Isotipo size={56} title={null} />
        <p role="status" className="text-small font-medium text-muted-foreground">
          Un momento…
        </p>
      </main>
    )
  }

  // A request that is no longer there: expired, answered, or someone else's.
  if (isNotFound(request.error) || isNotFound(answer.error)) {
    return <Explained title={EXPIRED.title} text={EXPIRED.text} />
  }

  if (request.isError) {
    return (
      <Explained
        title="No pudimos abrir la solicitud"
        text={problemMessage(request.error)}
        retry={() => void request.refetch()}
      />
    )
  }

  const { client_name: client, redirect_host: host } = request.data
  // Answered: the browser is on its way back to Claude. Nothing here may start
  // another answer, which would only find the request gone.
  const leaving = answer.isSuccess && !stuck
  const busy = answer.isPending || answer.isSuccess
  // Which answer is on its way, for its button's spinner.
  const answering = answer.isPending || leaving
  const denying = answer.variables !== undefined && 'deny' in answer.variables
  const approving = answering && !denying

  const respond = (deny: boolean) => {
    if (busy) return
    answer.mutate(deny ? { id, deny: true } : { id, write }, {
      onSuccess: ({ redirect_to }) => {
        if (!goBack(redirect_to)) setStuck(true)
      },
    })
  }

  return (
    <AuthLayout
      title={`${client} quiere conectarse a chenchén`}
      subtitle={
        host ? (
          <>
            Al contestar vuelves a{' '}
            <span className="font-semibold text-foreground break-all">{host}</span>.
          </>
        ) : undefined
      }
    >
      <div className="flex flex-col gap-6">
        <section aria-labelledby={`${ids}-podra`} className="flex flex-col gap-3">
          <h2 id={`${ids}-podra`} className="text-body font-semibold">
            Lo que podrá hacer
          </h2>
          <ul className="flex flex-col gap-2">
            <li>
              <Permission icon={<Tile icon={EyeIcon} />}>
                <span className="text-body font-semibold">
                  Ver tus cuentas, movimientos y gastos
                </span>
                <span className="text-small font-medium text-muted-foreground">
                  Siempre.
                </span>
              </Permission>
            </li>
            <li>
              {/* The whole row is the switch's tap target. */}
              <label className="block cursor-pointer">
                <Permission
                  icon={<Tile icon={PencilLineIcon} />}
                  control={
                    <Switch
                      checked={write}
                      onCheckedChange={setWrite}
                      disabled={busy}
                      aria-labelledby={writeTitleId}
                      aria-describedby={writeHintId}
                    />
                  }
                >
                  <span id={writeTitleId} className="text-body font-semibold text-pretty">
                    Anotar compras, ordenar tus movimientos y subir los archivos de tu banco
                  </span>
                  <span
                    id={writeHintId}
                    className="text-small font-medium text-pretty text-muted-foreground"
                  >
                    {write
                      ? 'Las reglas y los saldos que proponga los apruebas tú, en chenchén.'
                      : 'Apagado: solo podrá ver.'}
                  </span>
                </Permission>
              </label>
            </li>
          </ul>
        </section>

        {answer.isError && (
          <Alert variant="attention">
            <CircleAlertIcon aria-hidden="true" />
            <AlertTitle>No pudimos enviar tu respuesta</AlertTitle>
            <AlertDescription>{problemMessage(answer.error)}</AlertDescription>
          </Alert>
        )}
        {stuck && (
          <Alert variant="attention">
            <CircleAlertIcon aria-hidden="true" />
            <AlertTitle>No pudimos volver a Claude</AlertTitle>
            <AlertDescription>Vuelve a conectar chenchén desde Claude.</AlertDescription>
          </Alert>
        )}

        <div className="flex flex-col gap-2">
          <Button size="lg" disabled={busy} onClick={() => respond(false)}>
            {approving && <Spinner aria-hidden="true" />}
            {approving ? 'Volviendo a Claude…' : 'Permitir'}
          </Button>
          <Button variant="ghost" size="lg" disabled={busy} onClick={() => respond(true)}>
            {answering && denying && <Spinner aria-hidden="true" />}
            Cancelar
          </Button>
        </div>

        {/* Said once the answer is on its way, for a screen reader. */}
        <p role="status" className="sr-only">
          {leaving ? 'Volviendo a Claude.' : ''}
        </p>

        <p className="text-small font-medium text-pretty text-muted-foreground">
          Lo desconectas cuando quieras en{' '}
          <span className="whitespace-nowrap">Ajustes › Conexiones</span>.
        </p>
      </div>
    </AuthLayout>
  )
}

/** One thing Claude could do: its tile, its words, and the switch when it has one. */
function Permission({
  icon,
  control,
  children,
}: {
  icon: ReactNode
  control?: ReactNode
  children: ReactNode
}) {
  return (
    <span
      className={
        control
          ? 'grid min-h-11 grid-cols-[2.5rem_minmax(0,1fr)_auto] items-center gap-3 py-1'
          : 'grid min-h-11 grid-cols-[2.5rem_minmax(0,1fr)] items-center gap-3 py-1'
      }
    >
      {icon}
      <span className="flex min-w-0 flex-col">{children}</span>
      {control}
    </span>
  )
}

/**
 * Nothing to answer: a refusal, a request that is gone, or one that could not be
 * read. The way out is home (Inicio, or the portada signed out); a failure to read
 * offers "Reintentar" first.
 */
function Explained({
  title,
  text,
  retry,
}: {
  title: string
  text: string
  retry?: () => void
}) {
  const { status } = useSession()
  const signedIn = status === 'signed-in'
  const home = (
    <Link to={signedIn ? HOME_PATH : '/'}>
      {signedIn ? 'Ir a Inicio' : 'Ir a la portada'}
    </Link>
  )
  return (
    <AuthLayout title={title} subtitle={text}>
      <div className="flex flex-col gap-2">
        {retry ? (
          <>
            <Button size="lg" onClick={retry}>
              Reintentar
            </Button>
            <Button asChild variant="ghost" size="lg">
              {home}
            </Button>
          </>
        ) : (
          <Button asChild size="lg">
            {home}
          </Button>
        )}
      </div>
    </AuthLayout>
  )
}

/**
 * Sends the browser to Claude with the answer, and says whether it could. The address
 * comes from the API, which only ever answers with one of Claude's own; anything that
 * is not http(s) is not followed all the same.
 */
function goBack(to: string): boolean {
  let url: URL
  try {
    url = new URL(to)
  } catch {
    return false
  }
  if (url.protocol !== 'https:' && url.protocol !== 'http:') return false
  window.location.assign(url.href)
  return true
}
