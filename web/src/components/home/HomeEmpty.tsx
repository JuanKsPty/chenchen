/**
 * <HomeEmpty className? />
 *
 * Inicio before the first import (spec emptyState, voice.emptyState): the
 * illustration, "Importa tu primer estado de cuenta", the primary "Importar estado
 * de cuenta" (the app's one import dialog, useImport) and a ghost "Conectar
 * Interactive Brokers" that opens LinkIBKRDialog. In a surface card, like the first-run
 * state of Cuentas, Movimientos and Gastos.
 */
import { FileUpIcon } from 'lucide-react'

import { useImport } from '@/components/import/ImportProvider'
import { EmptyState } from '@/components/kit'
import { LinkIBKRDialog } from '@/components/LinkIBKRDialog'
import { Button } from '@/components/ui/button'

export function HomeEmpty({ className }: { className?: string }) {
  const { openImport } = useImport()

  return (
    <EmptyState
      framed
      illustration="import"
      title="Importa tu primer estado de cuenta"
      description="Sube el archivo que te da tu banco y aquí te lo dejamos ordenadito."
      action={{
        label: 'Importar estado de cuenta',
        icon: FileUpIcon,
        onClick: () => openImport(),
      }}
      className={className}
    >
      <LinkIBKRDialog
        trigger={
          <Button variant="ghost" className="-mt-1 w-full md:w-auto">
            Conectar Interactive Brokers
          </Button>
        }
      />
    </EmptyState>
  )
}
