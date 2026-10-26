/**
 * <SearchField value onValueChange label? placeholder? className? inputClassName?
 *              ...inputProps />
 *
 *   value          the text (controlled)
 *   onValueChange  called on every keystroke and with "" when cleared. Debounce in
 *                  the page (Movimientos waits 300ms before searching).
 *   label          the visually hidden <label>; default "Buscar movimientos"
 *   placeholder    default "Busca un comercio, monto o nota"
 *   className      on the wrapper (width: max-w-[35rem] on desktop Movimientos)
 *   other props    go to the <input type="search"> (id, autoFocus, onKeyDown…)
 *
 * Spec input/search: 48px pill on mobile, 44 from md, surface2, no border, padding
 * 0 16 0 44, Figtree 16/24. Search icon 20 in muted, 16 from the left. With text a
 * clear button appears (44 circle below lg, 36 from lg, aria-label "Borrar
 * búsqueda") that empties it and puts the focus back in the field.
 */
import { SearchIcon, XIcon } from 'lucide-react'
import { useId, useRef, type ComponentProps } from 'react'

import { Input } from '@/components/ui/input'
import { cn } from '@/lib/utils'

export function SearchField({
  value,
  onValueChange,
  label = 'Buscar movimientos',
  placeholder = 'Busca un comercio, monto o nota',
  className,
  inputClassName,
  id,
  ...props
}: Omit<ComponentProps<'input'>, 'value' | 'onChange' | 'type'> & {
  value: string
  onValueChange: (value: string) => void
  label?: string
  inputClassName?: string
}) {
  const generatedId = useId()
  const inputId = id ?? generatedId
  const input = useRef<HTMLInputElement>(null)

  return (
    <div className={cn('relative w-full', className)}>
      <label htmlFor={inputId} className="sr-only">
        {label}
      </label>
      <SearchIcon
        aria-hidden="true"
        className="pointer-events-none absolute top-1/2 left-4 size-5 -translate-y-1/2 text-muted-foreground"
      />
      <Input
        {...props}
        ref={input}
        id={inputId}
        type="search"
        value={value}
        onChange={(event) => onValueChange(event.target.value)}
        placeholder={placeholder}
        autoComplete="off"
        className={cn(
          'pl-11 [&::-webkit-search-cancel-button]:appearance-none',
          value ? 'pr-12 md:pr-11' : 'pr-4',
          inputClassName,
        )}
      />
      {value && (
        <button
          type="button"
          aria-label="Borrar búsqueda"
          onClick={() => {
            onValueChange('')
            input.current?.focus()
          }}
          className="absolute top-1/2 right-0.5 inline-flex size-11 -translate-y-1/2 items-center justify-center rounded-full text-muted-foreground transition-colors duration-180 ease-out hover:bg-line hover:text-foreground lg:right-1 lg:size-9"
        >
          <XIcon aria-hidden="true" className="size-5" />
        </button>
      )}
    </div>
  )
}
