/**
 * <ClaudeConnections />
 *
 * Ajustes › Conexiones › Claude: the connections the owner allowed on /conectar
 * (ADR-0004), newest first, each a SettingsRow:
 *   "Claude" or "Claude Code" · "Lee y anota" (or "Lee") · a chip with "Última vez
 *   hace 2 h", or "Conectado ayer" before its first use · [Desconectar], confirmed
 *   first in the inverse dialog DeleteRuleDialog uses. Disconnecting ends the
 *   connection and every token it holds: Claude asks to connect again.
 * With none, how to connect: the steps and the connector's address with [Copiar]. The
 * address is the MCP resource the API names in its metadata (useMcpResource), not
 * this site's origin; when that cannot be read, the steps go without it.
 *
 * Without the MCP (no PUBLIC_URL) its routes do not exist, the list is a 404, and
 * the group is left out: there is nothing to connect to.
 */
import { PlugIcon } from 'lucide-react'
import { useId, useRef, useState, type ReactNode } from 'react'
import { toast } from 'sonner'

import { isNotFound } from '@/api/client'
import type { ClaudeConnection } from '@/api/types'
import { CopyField } from '@/components/CopyField'
import { Badge } from '@/components/ui/badge'
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
import { relativeTime } from '@/lib/format'
import { useClaudeConnections, useDisconnectClaude, useMcpResource } from '@/lib/queries'
import { focusWhenReady } from './focus'
import { problemMessage } from './problem'
import { SettingsRow } from './SettingsRow'

export function ClaudeConnections() {
  const connections = useClaudeConnections()
  const headingId = useId()
  const [confirming, setConfirming] = useState<ClaudeConnection | null>(null)

  if (connections.isPending) return null
  if (isNotFound(connections.error)) return null

  const grants = connections.data?.grants ?? []

  let body: ReactNode
  if (connections.isError) {
    body = (
      <p role="alert" className="text-body text-pretty text-muted-foreground">
        {problemMessage(connections.error)}
      </p>
    )
  } else if (grants.length > 0) {
    body = (
      <ul className="flex flex-col gap-4 pt-2">
        {grants.map((grant) => (
          <li key={grant.id}>
            <SettingsRow
              icon={PlugIcon}
              title={grant.client_name}
              headingLevel={4}
              description={grant.scopes.includes('write') ? 'Lee y anota' : 'Lee'}
              meta={<Badge>{lastSeen(grant)}</Badge>}
              actions={
                <Button
                  variant="plain"
                  aria-label={`Desconectar ${grant.client_name}`}
                  onClick={() => setConfirming(grant)}
                >
                  Desconectar
                </Button>
              }
            />
          </li>
        ))}
      </ul>
    )
  } else {
    body = <HowToConnect />
  }

  return (
    <section aria-labelledby={headingId} className="flex flex-col gap-1 pt-4">
      <h3
        id={headingId}
        tabIndex={-1}
        className="text-body font-semibold text-foreground outline-none"
      >
        Claude
      </h3>
      {body}
      <DisconnectDialog
        connection={confirming}
        onOpenChange={(open) => {
          if (!open) setConfirming(null)
        }}
        onDisconnected={() => focusWhenReady(() => document.getElementById(headingId))}
      />
    </section>
  )
}

/** "Última vez hace 2 h", or, never used yet, when it was connected. */
function lastSeen(grant: ClaudeConnection): string {
  return grant.last_used_at
    ? `Última vez ${relativeTime(grant.last_used_at)}`
    : `Conectado ${relativeTime(grant.created_at)}`
}

/** A claude.ai label, set apart so the steps can be scanned. */
function ClaudeLabel({ children }: { children: ReactNode }) {
  return <span className="font-semibold text-foreground">{children}</span>
}

/** The connector's address, when the metadata names one that can be pasted. */
function connectorAddress(resource: unknown): string | null {
  return typeof resource === 'string' && /^https?:\/\//.test(resource) ? resource : null
}

function HowToConnect() {
  const resource = useMcpResource()
  // Same origin and tiny: wait for it rather than change the steps under the reader.
  if (resource.isPending) return null
  const address = connectorAddress(resource.data?.resource)
  return (
    <div className="flex flex-col gap-3">
      <p className="text-body text-pretty text-muted-foreground">
        Pregúntale a Claude por tu dinero y pídele que anote tus compras.
      </p>
      <ol className="flex list-decimal flex-col gap-2 pl-5 text-small font-medium text-pretty text-muted-foreground">
        <li>
          En claude.ai, abre <ClaudeLabel>Conectores</ClaudeLabel> y agrega uno
          {address ? ' con esta dirección.' : ' para chenchén.'}
        </li>
        <li>Cuando Claude te lo pida, entra a chenchén y elige qué puede hacer.</li>
      </ol>
      {address && (
        <CopyField
          label="Dirección del conector"
          copyLabel="Copiar dirección del conector"
          value={address}
        />
      )}
      <p className="text-small font-medium text-pretty text-muted-foreground">
        Queda conectado también en la app de Claude y en Claude Code.
      </p>
    </div>
  )
}

/**
 * The confirmation before a connection ends, as DeleteRuleDialog confirms a rule:
 * a secondary "Cancelar" and the inverse "Desconectar". It keeps showing the
 * connection's name while it fades out. While the request runs there is no way out,
 * so its answer always lands here. After it, the row is gone: focus goes where
 * onDisconnected puts it; cancelled, back to the button that opened it.
 */
function DisconnectDialog({
  connection,
  onOpenChange,
  onDisconnected,
}: {
  connection: ClaudeConnection | null
  onOpenChange: (open: boolean) => void
  onDisconnected: () => void
}) {
  const disconnect = useDisconnectClaude()
  const opener = useRef<HTMLElement | null>(null)
  const done = useRef(false)
  const [last, setLast] = useState(connection)
  if (connection && connection !== last) setLast(connection)
  const shown = connection ?? last

  function close() {
    onOpenChange(false)
    disconnect.reset()
  }

  function confirm() {
    if (!shown || disconnect.isPending) return
    const name = shown.client_name
    disconnect.mutate(shown.id, {
      onSuccess: () => {
        done.current = true
        close()
        toast(`Desconectaste ${name}.`)
      },
    })
  }

  return (
    <Dialog
      open={connection !== null}
      onOpenChange={(open) => {
        if (!open && !disconnect.isPending) close()
      }}
    >
      <DialogContent
        className="sm:max-w-104"
        showCloseButton={!disconnect.isPending}
        onOpenAutoFocus={() => {
          done.current = false
          const active = document.activeElement
          opener.current =
            active instanceof HTMLElement && active !== document.body ? active : null
        }}
        onCloseAutoFocus={(event) => {
          // After a disconnect the opener's row is on its way out (the list
          // refetches), so never send focus back to it.
          const opened = opener.current
          opener.current = null
          if (done.current) {
            done.current = false
            event.preventDefault()
            onDisconnected()
            return
          }
          if (opened?.isConnected) {
            event.preventDefault()
            opened.focus()
          }
        }}
      >
        <DialogHeader>
          <DialogTitle>¿Desconectar {shown?.client_name}?</DialogTitle>
          <DialogDescription>
            Deja de ver y anotar en tu chenchén desde ahora. Para volver a usarlo, conéctalo
            otra vez desde Claude.
          </DialogDescription>
        </DialogHeader>

        {disconnect.isError && (
          <p role="alert" className="text-small font-medium text-destructive">
            {problemMessage(disconnect.error)}
          </p>
        )}

        <DialogFooter>
          <Button variant="secondary" onClick={close} disabled={disconnect.isPending}>
            Cancelar
          </Button>
          <Button variant="destructive" onClick={confirm} disabled={disconnect.isPending}>
            {disconnect.isPending && <Spinner />}
            Desconectar
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
