/**
 * <ConnectIBKRRow another? />
 *
 *   another  the Inversiones group already has an account: reads "Conectar otra
 *            cuenta" instead of "Conectar Interactive Brokers"
 *
 * The last row of the Inversiones group: a pill row the size of an account row's
 * content, a pink plus tile and the label in primary-text (a ghost action, never a
 * second primary next to the bar's Importar). Opens LinkIBKRDialog.
 */
import { PlusIcon } from 'lucide-react'

import { Tile } from '@/components/kit'
import { LinkIBKRDialog } from '@/components/LinkIBKRDialog'

export function ConnectIBKRRow({ another = false }: { another?: boolean }) {
  return (
    <LinkIBKRDialog
      trigger={
        <button
          type="button"
          className="grid min-h-15 w-full grid-cols-[2.75rem_minmax(0,1fr)] items-center gap-3 rounded-full px-4 py-2 text-left text-body font-semibold text-primary-text transition-colors duration-180 ease-out hover:bg-muted"
        >
          <Tile icon={PlusIcon} pending size="md" />
          {another ? 'Conectar otra cuenta' : 'Conectar Interactive Brokers'}
        </button>
      }
    />
  )
}
