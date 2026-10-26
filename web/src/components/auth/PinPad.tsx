/**
 * <PinPad onComplete length? className? />
 *
 *   onComplete   (pin) => Promise<string | null>. Called on the last digit. While
 *                it runs the pad is locked and says "Entrando…". Resolve null on
 *                success (or when the page moves on by itself); resolve a message
 *                to clear the digits, shake the dots and show it in attention.
 *   length       digits, 6 by default (the server takes exactly 6).
 *
 * Spec components.pinPad: six 14px dots 16 apart (an empty one is a 2px ring in
 * muted text, a typed one is veranera), exposed as one role=img ("PIN: 2 de 6
 * dígitos"). Under them, a polite live line for "Entrando…" and errors. The keypad
 * is a 3×4 grid of real 72px circular buttons (24 across, 16 down) on surface2, the
 * digit in Gabarito 700 32/36; pressed turns primary-soft with primary-text. The
 * last row is an empty cell, 0 and "Borrar". The hardware digit keys and Backspace
 * work too, and flash the matching key.
 *
 * A wrong PIN shakes the dots for 300ms (animate-shake). Under reduced motion the
 * global base rule cuts every CSS animation to nothing, so the dots and the message
 * fade in through element.animate() instead, which that rule does not touch.
 */
import { DeleteIcon } from 'lucide-react'
import { useCallback, useEffect, useRef, useState } from 'react'

import { cn } from '@/lib/utils'

const KEYS = ['1', '2', '3', '4', '5', '6', '7', '8', '9', '', '0', 'del'] as const
const FLASH_MS = 160
const DOT_PITCH = 30 // a 14px dot and a 16px gap

export function PinPad({
  onComplete,
  length = 6,
  className,
}: {
  onComplete: (pin: string) => Promise<string | null>
  length?: number
  className?: string
}) {
  const [pin, setPin] = useState('')
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [shakes, setShakes] = useState(0)
  const [flash, setFlash] = useState<string | null>(null)

  const dotsRef = useRef<SVGSVGElement>(null)
  const messageRef = useRef<HTMLParagraphElement>(null)
  const flashTimer = useRef<number | undefined>(undefined)
  // The latest values, for the window keydown listener and for press(), which must
  // not see a stale PIN when two keys arrive between renders.
  const pinRef = useRef('')
  const pendingRef = useRef(false)
  const completeRef = useRef(onComplete)
  useEffect(() => {
    completeRef.current = onComplete
  })

  useEffect(() => () => window.clearTimeout(flashTimer.current), [])

  const submit = useCallback(async (value: string) => {
    pendingRef.current = true
    setPending(true)
    let message: string | null
    try {
      message = await completeRef.current(value)
    } catch {
      message = 'Algo falló de nuestro lado. Intenta otra vez en un momento.'
    }
    pendingRef.current = false
    setPending(false)
    if (message) {
      pinRef.current = ''
      setPin('')
      setError(message)
      setShakes((n) => n + 1)
    }
  }, [])

  const press = useCallback(
    (key: string) => {
      if (pendingRef.current) return
      window.clearTimeout(flashTimer.current)
      setFlash(key)
      flashTimer.current = window.setTimeout(() => setFlash(null), FLASH_MS)

      const current = pinRef.current
      if (key === 'del') {
        pinRef.current = current.slice(0, -1)
        setPin(pinRef.current)
        return
      }
      if (current.length >= length) return
      const next = current + key
      pinRef.current = next
      setPin(next)
      if (current === '') setError(null)
      if (next.length === length) void submit(next)
    },
    [length, submit],
  )

  // Hardware keys: a phone with a keyboard, or anyone testing on a laptop.
  useEffect(() => {
    function onKeyDown(event: KeyboardEvent) {
      if (event.altKey || event.ctrlKey || event.metaKey) return
      const target = event.target as HTMLElement | null
      if (target?.closest('input, textarea, select, [contenteditable="true"]')) return
      if (/^[0-9]$/.test(event.key)) {
        event.preventDefault()
        press(event.key)
      } else if (event.key === 'Backspace') {
        event.preventDefault()
        press('del')
      }
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [press])

  // The reduced-motion stand-in for the shake.
  useEffect(() => {
    if (shakes === 0) return
    if (!window.matchMedia('(prefers-reduced-motion: reduce)').matches) return
    for (const el of [dotsRef.current, messageRef.current]) {
      el?.animate([{ opacity: 0.15 }, { opacity: 1 }], {
        duration: 240,
        easing: 'ease-out',
      })
    }
  }, [shakes])

  const filled = pin.length

  return (
    <div className={cn('flex flex-col items-center', className)}>
      <svg
        // A new element per wrong PIN, so the shake plays again each time.
        key={shakes}
        ref={dotsRef}
        width={DOT_PITCH * (length - 1) + 18}
        height={18}
        viewBox={`0 0 ${DOT_PITCH * (length - 1) + 18} 18`}
        role="img"
        aria-label={`PIN: ${filled} de ${length} dígitos`}
        className={cn('block', shakes > 0 && 'animate-shake')}
      >
        {Array.from({ length }, (_, i) =>
          i < filled ? (
            <circle key={i} cx={9 + i * DOT_PITCH} cy={9} r={7} className="fill-primary" />
          ) : (
            <circle
              key={i}
              cx={9 + i * DOT_PITCH}
              cy={9}
              r={6}
              strokeWidth={2}
              className="fill-none stroke-muted-foreground"
            />
          ),
        )}
      </svg>

      <p
        ref={messageRef}
        aria-live="polite"
        className={cn(
          'mt-2 min-h-5 text-center text-small font-medium',
          error && !pending ? 'text-attention' : 'text-muted-foreground',
        )}
      >
        {pending ? 'Entrando…' : error}
      </p>

      <div
        role="group"
        aria-label="Teclado del PIN"
        // Row gap 16, or 12 on a short viewport (see SignInPage).
        className="mt-4 grid w-66 grid-cols-3 gap-x-6 gap-y-4 [@media(max-height:45rem)]:gap-y-3"
      >
        {KEYS.map((key, index) => {
          if (key === '') return <span key={index} aria-hidden="true" className="size-18" />
          const pressed = flash === key
          if (key === 'del') {
            return (
              <button
                key={key}
                type="button"
                aria-label="Borrar"
                aria-disabled={pending || undefined}
                data-pressed={pressed || undefined}
                onClick={() => press('del')}
                className={cn(
                  'flex size-18 items-center justify-center rounded-full bg-transparent text-foreground transition-colors duration-180 ease-out',
                  'hover:bg-muted active:bg-muted data-pressed:bg-muted aria-disabled:cursor-wait',
                )}
              >
                <DeleteIcon aria-hidden="true" className="size-6" />
              </button>
            )
          }
          return (
            <button
              key={key}
              type="button"
              aria-disabled={pending || undefined}
              data-pressed={pressed || undefined}
              onClick={() => press(key)}
              className={cn(
                'flex size-18 items-center justify-center rounded-full bg-muted font-display text-title font-bold text-foreground tabular-nums transition-colors duration-180 ease-out select-none',
                'hover:bg-line active:bg-primary-soft active:text-primary-text',
                'data-pressed:bg-primary-soft data-pressed:text-primary-text aria-disabled:cursor-wait',
              )}
            >
              {key}
            </button>
          )
        })}
      </div>
    </div>
  )
}
