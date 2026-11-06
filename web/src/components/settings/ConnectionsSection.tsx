/**
 * <ConnectionsSection className? />
 *
 * Ajustes › Conexiones: every brokerage account linked to Interactive Brokers, with
 * how its last sync went, and "Conectar Interactive Brokers" (LinkIBKRDialog). Then,
 * under "Atajos", every account whose iPhone shortcut has a key, when there is one.
 *
 * An Interactive Brokers row links to the account's page, where syncing and changing
 * the Flex Query live. The status comes from the account's link (useInvestmentLink):
 *   ok     "Sincronizado hace 2 h" (the kit's StatusChip)
 *   error  "No se pudo sincronizar", neutral chip with the icon in attention
 *   never  "Sin sincronizar todavía", neutral
 * While the link loads, `account.last_synced_at` stands in when the server sent it.
 *
 * An Atajos row links to the account's page with "Anotar compras con Atajos" open
 * (`?atajo=1`), where the key is made again or turned off. Its neutral chip says how
 * the shortcut is doing: "Última compra hace 2 h", "Probada, sin compras todavía" or
 * "Sin probar todavía".
 *
 * Last, under "Claude", the connections the owner allowed Claude, each with
 * [Desconectar], or how to connect it (ClaudeConnections). Left out when this
 * deployment has no MCP.
 */
import {
  ChevronRightIcon,
  CircleAlertIcon,
  PlusIcon,
  RefreshCwIcon,
  SmartphoneNfcIcon,
} from 'lucide-react'
import { useId, type ReactNode } from 'react'
import { Link } from 'react-router-dom'

import type { Account } from '@/api/types'
import { LinkIBKRDialog } from '@/components/LinkIBKRDialog'
import { AccountRowSubtitle, SectionCard, StatusChip, Tile } from '@/components/kit'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { accountRowTitle, hasShortcut, shortcutNote } from '@/lib/account'
import { accountHref } from '@/lib/nav'
import { useAccounts, useInvestmentLink } from '@/lib/queries'
import { ClaudeConnections } from './ClaudeConnections'
import { problemMessage } from './problem'

export function ConnectionsSection({ className }: { className?: string }) {
  const accounts = useAccounts()
  const shortcutsId = useId()
  const brokers = (accounts.data?.accounts ?? []).filter(
    (account) => account.type === 'brokerage',
  )
  const shortcuts = (accounts.data?.accounts ?? []).filter(hasShortcut)

  return (
    <SectionCard title="Conexiones" className={className}>
      {accounts.isPending ? (
        <div className="flex items-center gap-3 py-3" aria-hidden="true">
          <Skeleton className="size-11 shrink-0 rounded-full" />
          <div className="flex flex-1 flex-col gap-2">
            <Skeleton className="h-4 w-40" />
            <Skeleton className="h-3.5 w-28" />
          </div>
        </div>
      ) : accounts.isError ? (
        <p role="alert" className="text-body text-pretty text-muted-foreground">
          {problemMessage(accounts.error)}
        </p>
      ) : brokers.length > 0 ? (
        <ul className="-mx-3 flex flex-col gap-1 md:-mx-4">
          {brokers.map((account) => (
            <li key={account.id}>
              <ConnectionRow
                account={account}
                to={accountHref(account.id)}
                status={<SyncChip account={account} />}
              />
            </li>
          ))}
        </ul>
      ) : (
        <p className="text-body text-pretty text-muted-foreground">
          Conecta tu cuenta de Interactive Brokers y ves tus inversiones al día.
        </p>
      )}

      <div className="-ml-3 pt-2">
        <LinkIBKRDialog
          trigger={
            <Button variant="ghost">
              <PlusIcon aria-hidden="true" />
              {brokers.length > 0 ? 'Conectar otra cuenta' : 'Conectar Interactive Brokers'}
            </Button>
          }
        />
      </div>

      {shortcuts.length > 0 && (
        <section aria-labelledby={shortcutsId} className="flex flex-col gap-1 pt-4">
          <h3 id={shortcutsId} className="text-body font-semibold">
            Atajos
          </h3>
          <ul className="-mx-3 flex flex-col gap-1 md:-mx-4">
            {shortcuts.map((account) => (
              <li key={account.id}>
                <ConnectionRow
                  account={account}
                  to={accountHref(account.id, { shortcut: true })}
                  status={
                    account.shortcut && (
                      <Badge className="max-w-full">
                        <SmartphoneNfcIcon aria-hidden="true" />
                        <span className="truncate">{shortcutNote(account.shortcut)}</span>
                      </Badge>
                    )
                  }
                />
              </li>
            ))}
          </ul>
        </section>
      )}

      <ClaudeConnections />
    </SectionCard>
  )
}

function ConnectionRow({
  account,
  to,
  status,
}: {
  account: Account
  to: string
  /** The chip under the account's name. */
  status: ReactNode
}) {
  return (
    <Link
      to={to}
      className="grid grid-cols-[2.75rem_minmax(0,1fr)_auto] items-center gap-3 rounded-full px-3 py-3 text-foreground md:px-4 transition-colors duration-180 ease-out hover:bg-muted active:bg-muted"
    >
      <Tile institution={account.institution} />
      <span className="flex min-w-0 flex-col">
        <span className="truncate text-body font-semibold">{accountRowTitle(account)}</span>
        <AccountRowSubtitle account={account} />
        <span className="mt-1 flex">{status}</span>
      </span>
      <ChevronRightIcon aria-hidden="true" className="size-5 text-muted-foreground" />
    </Link>
  )
}

function SyncChip({ account }: { account: Account }) {
  const link = useInvestmentLink(account.id)
  const status = link.data?.last_sync_status
  const syncedAt = link.data?.last_synced_at ?? account.last_synced_at ?? undefined

  if (status === 'error') {
    return (
      <Badge>
        <CircleAlertIcon aria-hidden="true" className="text-attention" />
        No se pudo sincronizar
      </Badge>
    )
  }
  if (syncedAt && status !== 'never') {
    return <StatusChip status={{ kind: 'sincronizado', at: syncedAt }} />
  }
  if (status === 'never') {
    return (
      <Badge>
        <RefreshCwIcon aria-hidden="true" />
        Sin sincronizar todavía
      </Badge>
    )
  }
  return null
}
