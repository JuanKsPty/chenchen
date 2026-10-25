import { useEffect, useRef, useState } from 'react'

import { AssistantPanel } from '@/components/assistant/AssistantPanel'
import { useAssistant } from '@/components/assistant/AssistantProvider'
import { Sheet, SheetContent, SheetTitle } from '@/components/ui/sheet'
import { useReturnFocus } from '@/lib/focus'
import { cn } from '@/lib/utils'
import { PUSH_QUERY, useMediaQuery } from './media'

/**
 * Where the assistant shows, closed by default. One `open` (useAssistant) drives
 * whichever surface the width calls for; the conversation lives in the provider, so
 * crossing a breakpoint never interrupts a reply.
 *
 *   desktop (lg up)  <AssistantPanel variant="panel">: a 400px floating card, 80px
 *                    from the top and 16 from the other edges, surface and raised
 *                    shadow. From 1280px the shell pads the page 432px on the right
 *                    so nothing hides under it; narrower, it floats over the scrim,
 *                    the scrim closes it, and the page under it is inert (AppShell).
 *   mobile           <AssistantPanel variant="sheet">: a full-screen dialog sliding
 *                    up in 280ms, square once it arrives. It pads the safe areas
 *                    itself, so the panel inside does not.
 *
 * Escape closes either. The panel's own header carries "Cerrar asistente". Both
 * focus their own container on opening (not the first button) and give focus back
 * to whatever opened them on closing.
 */
export function AssistantSurface({ desktop }: { desktop: boolean }) {
  const { open, setOpen } = useAssistant()
  const close = () => setOpen(false)

  if (desktop) return open ? <AssistantAside onClose={close} /> : null
  return <AssistantSheet open={open} onOpenChange={setOpen} />
}

function AssistantAside({ onClose }: { onClose: () => void }) {
  const pushes = useMediaQuery(PUSH_QUERY)
  const ref = useRef<HTMLElement>(null)

  // Focus moves into the panel when it opens and goes back where it came from (the
  // Asistente button, usually) when it closes. That element is read while rendering:
  // by the time an effect runs, a floating panel has made the page inert (AppShell)
  // and the browser has already taken the focus off it.
  const [previous] = useState(() => document.activeElement)
  useEffect(() => {
    ref.current?.focus({ preventScroll: true })
    return () => {
      if (previous instanceof HTMLElement && previous.isConnected) {
        previous.focus({ preventScroll: true })
      }
    }
  }, [previous])

  return (
    <>
      {!pushes && (
        <div
          aria-hidden="true"
          onClick={onClose}
          className="fixed inset-0 z-40 bg-scrim duration-280 ease-sheet animate-in fade-in-0"
        />
      )}
      <aside
        ref={ref}
        aria-label="Asistente"
        tabIndex={-1}
        onKeyDown={(event) => {
          if (event.key === 'Escape' && !event.defaultPrevented) {
            event.stopPropagation()
            onClose()
          }
        }}
        className={cn(
          'fixed top-20 right-4 bottom-4 z-50 flex w-100 max-w-[calc(100vw-2rem)] flex-col overflow-hidden rounded-2xl bg-card shadow-raised outline-none',
          'duration-280 ease-sheet animate-in fade-in-0 slide-in-from-right-8',
        )}
      >
        <AssistantPanel variant="panel" onClose={onClose} className="min-h-0 flex-1" />
      </aside>
    </>
  )
}

function AssistantSheet({
  open,
  onOpenChange,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  // Rounded on top while it travels, square once it covers the screen.
  const [settled, setSettled] = useState(false)
  if (!open && settled) setSettled(false)
  const returnFocus = useReturnFocus()

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        side="bottom"
        showCloseButton={false}
        aria-describedby={undefined}
        // The sheet itself takes focus, not its first button: with a conversation
        // that would be "Borrar conversación". Tab then starts at the header. The
        // opener ("Abrir asistente") is remembered first, so it gets focus back.
        onOpenAutoFocus={(event) => {
          returnFocus.onOpenAutoFocus()
          event.preventDefault()
          if (event.target instanceof HTMLElement)
            event.target.focus({ preventScroll: true })
        }}
        onCloseAutoFocus={returnFocus.onCloseAutoFocus}
        onAnimationEnd={(event) => {
          if (open && event.target === event.currentTarget) setSettled(true)
        }}
        className={cn(
          'gap-0 bg-card p-0 pt-[env(safe-area-inset-top)]',
          'data-[side=bottom]:h-dvh data-[side=bottom]:max-h-dvh',
          settled ? 'data-[side=bottom]:rounded-none' : 'data-[side=bottom]:rounded-t-2xl',
        )}
      >
        <SheetTitle className="sr-only">Asistente</SheetTitle>
        <AssistantPanel
          variant="sheet"
          onClose={() => onOpenChange(false)}
          className="min-h-0 flex-1"
        />
      </SheetContent>
    </Sheet>
  )
}
