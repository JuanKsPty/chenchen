/**
 * <CopyField label copyLabel value />
 *
 *   label      the field's visible label ("Dirección", "Clave")
 *   copyLabel  the button's name for a screen reader ("Copiar dirección")
 *   value      the text, read-only
 *
 * A read-only field with its own [Copiar], for what the owner pastes somewhere else:
 * the shortcut's address and key, Claude's connector address. The toast says
 * "Listo: copiada" (or how to copy it by hand) and never repeats the text, which may
 * be a secret.
 */
import { CopyIcon } from 'lucide-react'
import { useId, useRef } from 'react'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Field, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'

const COPIED = 'Listo: copiada'
const NOT_COPIED = 'No pudimos copiarla. Ya está seleccionada: cópiala tú.'

/**
 * A read-only field with its own [Copiar]. The text can always be selected by hand
 * too: tapping the field selects all of it.
 */
export function CopyField({
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
