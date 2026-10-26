import { useLayoutEffect, useMemo, useRef, type PointerEvent } from 'react'

/** How far down a drag has to travel before letting go closes the sheet. */
const CLOSE_AFTER = 80

/**
 * Swipe down to close a bottom sheet (spec bottomSheet). Spread the result on the
 * sheet's handle or header (with `touch-none`, so the drag is not a scroll):
 *
 *   const swipe = useSwipeDown(() => onOpenChange(false))
 *   <SheetHeader className="touch-none" {...swipe}>…</SheetHeader>
 *
 * Only touch and pen drags count; a mouse closes with the scrim, Escape or "Cerrar".
 * The sheet follows the finger. Let go past 80px and it closes from where it is (the
 * exit animation continues from the dragged position); short of that it springs back.
 */
export function useSwipeDown(onClose: () => void) {
  const start = useRef<number | null>(null)
  const sheet = useRef<HTMLElement | null>(null)
  const close = useRef(onClose)
  useLayoutEffect(() => {
    close.current = onClose
  })

  return useMemo(() => {
    const finish = (event: PointerEvent<HTMLElement>) => {
      if (start.current === null) return
      const distance = event.clientY - start.current
      start.current = null
      const element = sheet.current
      sheet.current = null
      if (!element) return
      element.style.transition = ''
      if (distance > CLOSE_AFTER) {
        close.current()
      } else {
        element.style.transform = ''
      }
    }

    return {
      onPointerDown: (event: PointerEvent<HTMLElement>) => {
        if (event.pointerType === 'mouse') return
        // Not from a button inside the header (the close button keeps its tap).
        if ((event.target as HTMLElement).closest('button, a, input, textarea')) return
        start.current = event.clientY
        sheet.current = event.currentTarget.closest<HTMLElement>(
          '[data-slot="sheet-content"]',
        )
        event.currentTarget.setPointerCapture(event.pointerId)
      },
      onPointerMove: (event: PointerEvent<HTMLElement>) => {
        if (start.current === null || !sheet.current) return
        const distance = Math.max(0, event.clientY - start.current)
        sheet.current.style.transition = 'none'
        sheet.current.style.transform = `translateY(${distance}px)`
      },
      onPointerUp: finish,
      onPointerCancel: finish,
    }
  }, [])
}
