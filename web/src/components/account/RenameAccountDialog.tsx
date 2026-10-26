/**
 * <RenameAccountDialog account open onOpenChange />
 *
 * "Renombrar": the name the owner knows the account by, and for a bank account
 * whether it is a checking or a savings account (the importer cannot always tell).
 * Opened from the overflow menu, so it is controlled and gives focus back to whatever
 * opened it.
 */
import { CircleAlertIcon } from 'lucide-react'
import { useId, useState } from 'react'
import { toast } from 'sonner'

import { ApiError } from '@/api/client'
import type { Account } from '@/api/types'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Field, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import { Spinner } from '@/components/ui/spinner'
import { accountRowTitle } from '@/lib/account'
import { useReturnFocus } from '@/lib/focus'
import { useUpdateAccount } from '@/lib/queries'
import { MAX_ALIAS } from '@/lib/validate'

type BankType = 'checking' | 'savings'

const BANK_TYPES: { value: BankType; label: string }[] = [
  { value: 'checking', label: 'Corriente' },
  { value: 'savings', label: 'Ahorros' },
]

export function RenameAccountDialog({
  account,
  open,
  onOpenChange,
}: {
  account: Account
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const returnFocus = useReturnFocus()
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-104" {...returnFocus}>
        <RenameForm account={account} onDone={() => onOpenChange(false)} />
      </DialogContent>
    </Dialog>
  )
}

function RenameForm({ account, onDone }: { account: Account; onDone: () => void }) {
  const typeLabelId = useId()
  const bank = account.type === 'checking' || account.type === 'savings'
  const [alias, setAlias] = useState(account.alias ?? '')
  const [type, setType] = useState<BankType>(
    account.type === 'savings' ? 'savings' : 'checking',
  )
  const update = useUpdateAccount(account.id)

  const submit = (event: React.FormEvent) => {
    event.preventDefault()
    const patch: { alias?: string; type?: BankType } = {}
    if (alias.trim() !== (account.alias ?? '')) patch.alias = alias.trim()
    if (bank && type !== account.type) patch.type = type
    if (Object.keys(patch).length === 0) return onDone()
    update.mutate(patch, {
      onSuccess: () => {
        toast('Cambios guardados.')
        onDone()
      },
    })
  }

  const fieldError =
    update.error instanceof ApiError ? update.error.fields?.alias : undefined

  return (
    <form onSubmit={submit} className="flex flex-col gap-4">
      <DialogHeader>
        <DialogTitle>Renombrar cuenta</DialogTitle>
        <DialogDescription>
          Ponle el nombre con que la reconoces. Si lo dejas vacío, usamos el del banco.
        </DialogDescription>
      </DialogHeader>

      <Field>
        <FieldLabel htmlFor="account-alias">Nombre</FieldLabel>
        <Input
          id="account-alias"
          value={alias}
          maxLength={MAX_ALIAS}
          placeholder={accountRowTitle({ ...account, alias: undefined })}
          autoComplete="off"
          aria-invalid={fieldError ? true : undefined}
          onChange={(e) => setAlias(e.target.value)}
        />
      </Field>

      {bank && (
        <div className="flex flex-col gap-1">
          <p id={typeLabelId} className="text-body font-medium">
            Tipo de cuenta
          </p>
          <RadioGroup
            aria-labelledby={typeLabelId}
            value={type}
            onValueChange={(value) => setType(value as BankType)}
            className="-mx-3"
          >
            {BANK_TYPES.map((option) => (
              <label
                key={option.value}
                className="flex h-11 cursor-pointer items-center gap-3 rounded-full px-3 text-body font-medium transition-colors duration-180 ease-out hover:bg-muted"
              >
                <RadioGroupItem value={option.value} />
                {option.label}
              </label>
            ))}
          </RadioGroup>
        </div>
      )}

      {update.isError && (
        <Alert variant="attention">
          <CircleAlertIcon aria-hidden="true" />
          <AlertDescription className="text-foreground">
            {fieldError ??
              (update.error instanceof ApiError && update.error.status < 500
                ? update.error.message
                : 'Algo falló de nuestro lado. Tus datos están bien; intenta otra vez en un momento.')}
          </AlertDescription>
        </Alert>
      )}

      <DialogFooter>
        <Button type="button" variant="secondary" onClick={onDone}>
          Cancelar
        </Button>
        <Button type="submit" disabled={update.isPending}>
          {update.isPending && <Spinner />}
          Guardar
        </Button>
      </DialogFooter>
    </form>
  )
}
