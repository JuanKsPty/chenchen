/**
 * <DeleteRuleDialog rule onOpenChange fallbackFocus? />
 *
 *   rule           the rule to delete; non-null opens the dialog
 *   onOpenChange   called with false when it closes (Cancelar, Esc, the scrim, or
 *                  after deleting)
 *   onDeleted      after the server removed it (the page drops it from its form)
 *   fallbackFocus  where focus goes after deleting (the trash button that opened
 *                  it goes away with its row): given the deleted rule, return the
 *                  next row's button, or the form's text field
 *
 * The confirmation before a rule goes, per spec: a secondary "Cancelar" and the
 * inverse destructive "Eliminar regla" (never orange). Movements the rule already
 * filed keep their category; only future imports stop using it. Keeps showing the
 * rule's text while it fades out.
 */
import { useRef, useState } from 'react'
import { toast } from 'sonner'

import type { CategoryRule } from '@/api/types'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Spinner } from '@/components/ui/spinner'
import { useDeleteRule } from '@/lib/queries'
import { problemMessage } from './problem'

export function DeleteRuleDialog({
  rule,
  onOpenChange,
  onDeleted,
  fallbackFocus,
}: {
  rule: CategoryRule | null
  onOpenChange: (open: boolean) => void
  onDeleted?: (rule: CategoryRule) => void
  fallbackFocus?: (deleted: CategoryRule) => HTMLElement | null | undefined
}) {
  const remove = useDeleteRule()
  const opener = useRef<HTMLElement | null>(null)
  const deleted = useRef<CategoryRule | null>(null)
  const [last, setLast] = useState(rule)
  if (rule && rule !== last) setLast(rule)
  const shown = rule ?? last

  function close() {
    onOpenChange(false)
    remove.reset()
  }

  function confirm() {
    if (!shown) return
    const target = shown
    remove.mutate(target.id, {
      onSuccess: () => {
        deleted.current = target
        close()
        onDeleted?.(target)
        toast('Regla eliminada.')
      },
    })
  }

  return (
    <Dialog
      open={rule !== null}
      onOpenChange={(open) => {
        if (!open && !remove.isPending) close()
      }}
    >
      <DialogContent
        onOpenAutoFocus={() => {
          deleted.current = null
          const active = document.activeElement
          opener.current =
            active instanceof HTMLElement && active !== document.body ? active : null
        }}
        onCloseAutoFocus={(event) => {
          // After a delete the opener's row is on its way out (the list refetches),
          // so never send focus back to it.
          const gone = deleted.current
          const target = gone
            ? fallbackFocus?.(gone)
            : opener.current?.isConnected
              ? opener.current
              : null
          opener.current = null
          deleted.current = null
          if (target) {
            event.preventDefault()
            target.focus()
          }
        }}
      >
        <DialogHeader>
          <DialogTitle className="break-words">
            ¿Eliminar la regla «{shown?.match_text}»?
          </DialogTitle>
          <DialogDescription>
            Tus movimientos se quedan con la categoría que ya tienen. Lo próximo que
            importes ya no se ordena con ella.
          </DialogDescription>
        </DialogHeader>

        {remove.isError && (
          <p role="alert" className="text-small font-medium text-destructive">
            {problemMessage(remove.error)}
          </p>
        )}

        <DialogFooter>
          <Button variant="secondary" onClick={close} disabled={remove.isPending}>
            Cancelar
          </Button>
          <Button variant="destructive" onClick={confirm} disabled={remove.isPending}>
            {remove.isPending && <Spinner />}
            Eliminar regla
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
