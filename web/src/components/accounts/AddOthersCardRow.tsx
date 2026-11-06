/**
 * <AddOthersCardRow />
 *
 * The last row of Tarjetas de otros, built like ConnectIBKRRow: a pill row the size
 * of an account row's content, a pink plus tile and "Agregar tarjeta de otra persona"
 * in primary-text (a ghost action, never a second primary next to the bar's
 * Importar). The label stays the same with cards already there: "Agregar otra
 * tarjeta" would not say whose. Opens AddOthersCardDialog.
 */
import { PlusIcon } from 'lucide-react'

import { AddOthersCardDialog } from '@/components/accounts/AddOthersCardDialog'
import { Tile } from '@/components/kit'

export function AddOthersCardRow() {
  return (
    <AddOthersCardDialog
      trigger={
        <button
          type="button"
          className="grid min-h-15 w-full grid-cols-[2.75rem_minmax(0,1fr)] items-center gap-3 rounded-full px-4 py-2 text-left text-body font-semibold text-primary-text transition-colors duration-180 ease-out hover:bg-muted"
        >
          <Tile icon={PlusIcon} pending size="md" />
          Agregar tarjeta de otra persona
        </button>
      }
    />
  )
}
