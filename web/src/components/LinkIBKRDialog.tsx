/**
 * <LinkIBKRDialog trigger accountId? />   (signature frozen)
 *
 *   trigger    the element that opens it (Radix asChild): a ghost "Conectar
 *              Interactive Brokers", the Inversiones row, "Cambiar conexión"
 *   accountId  a linked brokerage account: replaces its Flex Query and token
 *              instead of creating a new account
 *
 * Connecting Interactive Brokers through a read-only Flex Query. The first link
 * creates the brokerage account and opens its page. A bottom sheet below lg, a
 * dialog from lg; the token is never shown again once saved.
 */
import { ChevronDownIcon, CircleAlertIcon } from 'lucide-react'
import { useId, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { toast } from 'sonner'

import { ApiError } from '@/api/client'
import { ResponsiveDialogContent } from '@/components/accounts/ResponsiveDialog'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Dialog, DialogClose, DialogTrigger } from '@/components/ui/dialog'
import { Field, FieldDescription, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Spinner } from '@/components/ui/spinner'
import { accountHref } from '@/lib/nav'
import { useLinkIBKR, useRelinkIBKR } from '@/lib/queries'

export function LinkIBKRDialog({
  trigger,
  accountId,
}: {
  trigger: React.ReactNode
  accountId?: string
}) {
  const [open, setOpen] = useState(false)
  const [ibkrAccountId, setIbkrAccountId] = useState('')
  const [flexQueryId, setFlexQueryId] = useState('')
  const [flexToken, setFlexToken] = useState('')
  const link = useLinkIBKR()
  const relink = useRelinkIBKR(accountId ?? '')
  const pending = link.isPending || relink.isPending
  const failure = link.error ?? relink.error
  const navigate = useNavigate()
  const formId = useId()
  const relinking = Boolean(accountId)

  const submit = (event: React.FormEvent) => {
    event.preventDefault()
    const done = () => {
      setOpen(false)
      setFlexToken('')
    }
    if (accountId) {
      relink.mutate(
        { flex_query_id: flexQueryId.trim(), flex_token: flexToken.trim() },
        {
          onSuccess: () => {
            done()
            toast('Listo: guardamos la conexión nueva.')
          },
        },
      )
    } else {
      link.mutate(
        {
          ibkr_account_id: ibkrAccountId.trim().toUpperCase(),
          flex_query_id: flexQueryId.trim(),
          flex_token: flexToken.trim(),
        },
        {
          onSuccess: (result) => {
            done()
            navigate(accountHref(result.account_id))
          },
        },
      )
    }
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        setOpen(next)
        if (!next) {
          // The token is a secret: it does not wait in the form for next time.
          setFlexToken('')
          link.reset()
          relink.reset()
        }
      }}
    >
      <DialogTrigger asChild>{trigger}</DialogTrigger>
      <ResponsiveDialogContent
        title={relinking ? 'Cambiar conexión' : 'Conectar Interactive Brokers'}
        description={
          relinking
            ? 'Pega la Query ID y el token nuevos. Tus posiciones y operaciones se quedan.'
            : 'Traemos tus posiciones y operaciones con una Flex Query de solo lectura: no movemos tu plata.'
        }
        footer={
          <>
            <DialogClose asChild>
              <Button variant="ghost" size="lg" className="lg:h-11">
                Cancelar
              </Button>
            </DialogClose>
            <Button
              type="submit"
              form={formId}
              size="lg"
              className="lg:h-11"
              disabled={pending}
            >
              {pending && <Spinner aria-hidden="true" />}
              {relinking ? 'Guardar' : 'Conectar'}
            </Button>
          </>
        }
      >
        <form id={formId} onSubmit={submit} className="flex flex-col gap-4">
          <fieldset className="flex flex-col gap-4" disabled={pending}>
            {!relinking && (
              <Field>
                <FieldLabel htmlFor={`${formId}-account`}>Número de cuenta</FieldLabel>
                <Input
                  id={`${formId}-account`}
                  value={ibkrAccountId}
                  onChange={(e) => setIbkrAccountId(e.target.value)}
                  placeholder="U1234567"
                  autoComplete="off"
                  autoCapitalize="characters"
                  spellCheck={false}
                  required
                />
              </Field>
            )}
            <Field>
              <FieldLabel htmlFor={`${formId}-query`}>Query ID</FieldLabel>
              <Input
                id={`${formId}-query`}
                value={flexQueryId}
                onChange={(e) => setFlexQueryId(e.target.value)}
                inputMode="numeric"
                autoComplete="off"
                spellCheck={false}
                required
              />
            </Field>
            <Field>
              <FieldLabel htmlFor={`${formId}-token`}>Token</FieldLabel>
              <Input
                id={`${formId}-token`}
                type="password"
                value={flexToken}
                onChange={(e) => setFlexToken(e.target.value)}
                autoComplete="off"
                aria-describedby={`${formId}-token-help`}
                required
              />
              <FieldDescription id={`${formId}-token-help`}>
                Lo guardamos cifrado y no lo volvemos a mostrar.
              </FieldDescription>
            </Field>
          </fieldset>

          <details className="group rounded-2xl bg-muted px-4 py-1">
            <summary className="flex min-h-11 cursor-pointer list-none items-center justify-between gap-3 text-body font-semibold text-foreground [&::-webkit-details-marker]:hidden">
              ¿Dónde encuentro estos datos?
              <ChevronDownIcon
                aria-hidden="true"
                className="size-5 shrink-0 text-muted-foreground transition-transform duration-180 ease-out group-open:rotate-180"
              />
            </summary>
            <ol className="flex list-decimal flex-col gap-2 pt-1 pb-3 pl-5 text-small font-medium text-pretty text-muted-foreground">
              <li>
                En Interactive Brokers, abre Performance &amp; Reports y luego Flex Queries.
              </li>
              <li>
                Crea una Activity Flex Query en XML, con período «Last 365 Calendar Days» y
                las secciones Open Positions, Trades, Cash Transactions y Cash Report.
              </li>
              <li>Copia su Query ID y, en Flex Web Service, genera el token.</li>
            </ol>
          </details>

          {failure && (
            <Alert variant="attention">
              <CircleAlertIcon />
              <AlertTitle>No pudimos conectar la cuenta</AlertTitle>
              <AlertDescription>
                {failure instanceof ApiError && failure.status < 500
                  ? failure.message
                  : 'Algo falló de nuestro lado. Tus datos están bien; intenta otra vez en un momento.'}
              </AlertDescription>
            </Alert>
          )}
        </form>
      </ResponsiveDialogContent>
    </Dialog>
  )
}
