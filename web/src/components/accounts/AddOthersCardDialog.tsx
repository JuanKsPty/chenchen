/**
 * <AddOthersCardDialog trigger />
 *
 *   trigger  the element that opens it (Radix asChild): the ghost row at the end of
 *            Tarjetas de otros (AddOthersCardRow), or a ghost button in Cuentas'
 *            empty state
 *
 * "Tarjeta que paga otra persona": the one account the owner adds by hand, a card
 * they use while somebody else gets its statements and pays it. Its purchases count
 * in their spending, never in what they owe nor in their net worth, and it lives only
 * on the purchases they note on it, by hand or with Atajos.
 *
 * Two fields: "¿Cómo la llamas?", its name (there is no bank's to fall back on), and
 * "Últimos 4 dígitos", which tell it apart from the owner's own cards. On success it
 * opens the new card's page with "Anotar compras con Atajos" (`?atajo=1`), the way
 * its purchases are meant to arrive.
 *
 * Both fields are checked before sending, together. A field the server refuses is
 * marked under that field, and so are digits the owner already has on a card (one
 * someone else pays, or one whose statements they import: a 409), with focus on the
 * first one; any other failure is an Alert above the buttons. Below lg a bottom sheet,
 * from lg a dialog (ResponsiveDialogContent), with "Agregar tarjeta" as its one
 * primary. Every opening starts blank.
 */
import { CircleAlertIcon } from 'lucide-react'
import { useId, useRef, useState, type ReactNode } from 'react'
import { useNavigate } from 'react-router-dom'
import { toast } from 'sonner'

import { ApiError } from '@/api/client'
import { serverProblems } from '@/components/account/AddPurchaseDialog'
import { ResponsiveDialogContent } from '@/components/accounts/ResponsiveDialog'
import { focusWhenReady } from '@/components/settings/focus'
import { problemMessage } from '@/components/settings/problem'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Dialog, DialogClose, DialogTrigger } from '@/components/ui/dialog'
import { Field, FieldDescription, FieldError, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Spinner } from '@/components/ui/spinner'
import { accountHref } from '@/lib/nav'
import { useAddOthersCard } from '@/lib/queries'
import { isClean, MAX_ALIAS, type Errors } from '@/lib/validate'

/** The fields, by the names the server reports problems under. */
const FIELDS = ['alias', 'last4'] as const
type CardField = (typeof FIELDS)[number]

/**
 * The server's 409s, both about the digits: a card someone else pays that already
 * ends in them, or a card whose statements the owner imports.
 */
const DIGITS_TAKEN = new Set(['hand_card_taken', 'card_imported'])

/**
 * What "Últimos 4 dígitos" keeps of what was typed or pasted: digits only, at most
 * four. A paste of a longer number ("4111 1111 1111 7788", or "•••• 7788" as Wallet
 * shows it) keeps its last four, the ones that tell the card apart. A digit typed
 * into a full field, wherever the cursor was, is refused and the field keeps what it
 * had, as a maxLength would: cutting it to four could drop a digit the owner meant to
 * keep. No maxLength itself: it would cut a paste short before the spaces and dots
 * are dropped.
 */
export function lastFour(value: string, previous: string): string {
  const digits = value.replace(/\D/g, '')
  if (digits.length <= 4) return digits
  return digits.length - previous.length > 1 ? digits.slice(-4) : previous
}

/** What is wrong before sending, both fields at once, in the server's own words. */
function cardProblems(alias: string, last4: string): Errors<CardField> {
  return {
    alias: alias.trim() ? undefined : 'Ponle un nombre a la tarjeta.',
    last4: /^\d{4}$/.test(last4)
      ? undefined
      : 'Escribe los últimos 4 dígitos de la tarjeta.',
  }
}

export function AddOthersCardDialog({ trigger }: { trigger: ReactNode }) {
  const [open, setOpen] = useState(false)
  // A blank form every time it opens. Counted on opening, not on closing, so what
  // was typed stays put while the dialog animates out.
  const [round, setRound] = useState(0)

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        setOpen(next)
        if (next) setRound((n) => n + 1)
      }}
    >
      <DialogTrigger asChild>{trigger}</DialogTrigger>
      <CardForm key={round} onDone={() => setOpen(false)} />
    </Dialog>
  )
}

function CardForm({ onDone }: { onDone: () => void }) {
  const formId = useId()
  const aliasId = `${formId}-alias`
  const last4Id = `${formId}-last4`
  const formRef = useRef<HTMLFormElement>(null)
  const submitRef = useRef<HTMLButtonElement>(null)
  const navigate = useNavigate()
  const add = useAddOthersCard()
  // One id per opening of the form: a send again after an answer that never came
  // is the same card, not a second one whose digits are "already taken".
  const [cardId] = useState(() => crypto.randomUUID())

  const [alias, setAlias] = useState('')
  const [last4, setLast4] = useState('')
  const [errors, setErrors] = useState<Errors<CardField>>({})
  const [failure, setFailure] = useState<string | null>(null)

  const clear = (field: CardField) => {
    if (errors[field]) setErrors((current) => ({ ...current, [field]: undefined }))
  }
  const focusFirstProblem = () =>
    focusWhenReady(() =>
      formRef.current?.querySelector<HTMLElement>('[aria-invalid="true"]'),
    )

  const submit = (event: React.FormEvent) => {
    event.preventDefault()
    if (add.isPending) return
    const found = cardProblems(alias, last4)
    setErrors(found)
    setFailure(null)
    if (!isClean(found)) {
      focusFirstProblem()
      return
    }
    add.mutate(
      { id: cardId, alias: alias.trim(), last4 },
      {
        onSuccess: (account) => {
          onDone()
          toast(`Listo: agregaste ${account.alias ?? alias.trim()}.`)
          // Its purchases are meant to arrive by the shortcut: set it up first.
          navigate(accountHref(account.id, { shortcut: true }))
        },
        onError: (error) => {
          const fields =
            error instanceof ApiError && DIGITS_TAKEN.has(error.code)
              ? { last4: error.message }
              : serverProblems(error, FIELDS)
          if (fields) {
            setErrors(fields)
            focusFirstProblem()
          } else {
            setFailure(problemMessage(error))
            focusWhenReady(() => submitRef.current)
          }
        },
      },
    )
  }

  const last4Hint = `${last4Id}-hint`
  const last4Error = `${last4Id}-error`

  return (
    <ResponsiveDialogContent
      title="Tarjeta que paga otra persona"
      description="Sus compras cuentan en tus gastos, pero no en lo que debes ni en tu patrimonio."
      footer={
        <>
          <DialogClose asChild>
            <Button variant="ghost" size="lg" className="lg:h-11">
              Cancelar
            </Button>
          </DialogClose>
          <Button
            ref={submitRef}
            type="submit"
            form={formId}
            size="lg"
            className="lg:h-11"
            // Not disabled: that would drop the focus while it saves, and an error
            // then leaves it nowhere.
            aria-disabled={add.isPending || undefined}
          >
            {add.isPending && <Spinner aria-hidden="true" />}
            Agregar tarjeta
          </Button>
        </>
      }
    >
      <form
        ref={formRef}
        id={formId}
        onSubmit={submit}
        noValidate
        className="flex flex-col gap-4"
      >
        <Field data-invalid={errors.alias ? true : undefined}>
          <FieldLabel htmlFor={aliasId}>¿Cómo la llamas?</FieldLabel>
          <Input
            id={aliasId}
            value={alias}
            maxLength={MAX_ALIAS}
            autoComplete="off"
            autoCapitalize="sentences"
            placeholder="Tarjeta de papá"
            aria-invalid={errors.alias ? true : undefined}
            aria-describedby={errors.alias ? `${aliasId}-error` : undefined}
            onChange={(event) => {
              setAlias(event.target.value)
              clear('alias')
            }}
          />
          {errors.alias && <FieldError id={`${aliasId}-error`}>{errors.alias}</FieldError>}
        </Field>

        <Field data-invalid={errors.last4 ? true : undefined}>
          <FieldLabel htmlFor={last4Id}>Últimos 4 dígitos</FieldLabel>
          <Input
            id={last4Id}
            value={last4}
            inputMode="numeric"
            autoComplete="off"
            spellCheck={false}
            aria-invalid={errors.last4 ? true : undefined}
            aria-describedby={errors.last4 ? `${last4Hint} ${last4Error}` : last4Hint}
            onChange={(event) => {
              setLast4(lastFour(event.target.value, last4))
              clear('last4')
            }}
          />
          <FieldDescription id={last4Hint}>Los ves en Wallet.</FieldDescription>
          {errors.last4 && <FieldError id={last4Error}>{errors.last4}</FieldError>}
        </Field>

        {failure && (
          <Alert variant="attention">
            <CircleAlertIcon aria-hidden="true" />
            <AlertTitle>No pudimos agregar la tarjeta</AlertTitle>
            <AlertDescription>{failure}</AlertDescription>
          </Alert>
        )}
      </form>
    </ResponsiveDialogContent>
  )
}
