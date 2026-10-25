/**
 * Ajustes (/ajustes), reached from the avatar menu.
 *
 *   Apariencia   Claro / Oscuro / Sistema
 *   Seguridad    the PIN and this phone (#seguridad; /seguridad redirects here)
 *   Conexiones   Interactive Brokers accounts and their last sync
 *   Reglas       the way into /gastos/reglas
 *
 * One column on a phone. When the page itself is 56rem or wider (lg and up, unless
 * the assistant panel takes the room), two: Apariencia and Seguridad on the left in
 * 7 of 12 columns, Conexiones and Reglas on the right in 5. The mobile bar already
 * says "Ajustes", so the h1 shows from lg only (PageHeader).
 */
import { useEffect, useRef } from 'react'
import { useLocation } from 'react-router-dom'

import { PageHeader } from '@/components/kit'
import { AppearanceSection } from '@/components/settings/AppearanceSection'
import { ConnectionsSection } from '@/components/settings/ConnectionsSection'
import { RulesSection } from '@/components/settings/RulesSection'
import { SecuritySection } from '@/components/settings/SecuritySection'
import { useSecurityStatus } from '@/lib/queries'
import { useDocumentTitle } from '@/lib/title'

const SECURITY_ID = 'seguridad'

export function SettingsPage() {
  useDocumentTitle('Ajustes')
  const security = useSecurityStatus()
  useScrollToWhenReady(SECURITY_ID, !security.isPending)

  return (
    <div className="flex flex-col gap-4 lg:gap-6">
      <PageHeader title="Ajustes" />
      <div className="@container">
        <div className="grid gap-4 lg:gap-6 @4xl:grid-cols-12 @4xl:items-start">
          <div className="flex min-w-0 flex-col gap-4 lg:gap-6 @4xl:col-span-7">
            <AppearanceSection />
            {/* Clear of the bar for the browser's own #seguridad jump too (a hash
                typed on this page): 56 + 16 below lg, 64 + 16 from lg. */}
            <SecuritySection id={SECURITY_ID} className="scroll-mt-18 lg:scroll-mt-20" />
          </div>
          <div className="flex min-w-0 flex-col gap-4 lg:gap-6 @4xl:col-span-5">
            <ConnectionsSection />
            <RulesSection />
          </div>
        </div>
      </div>
    </div>
  )
}

/**
 * The shell scrolls to a #hash on the first render after navigating, when the
 * Seguridad card still holds a skeleton. Once its content is in, scroll there again
 * (once per visit), 16px below whichever bar is showing, as the shell does.
 */
function useScrollToWhenReady(id: string, ready: boolean) {
  const location = useLocation()
  const scrolledFor = useRef<string | null>(null)

  useEffect(() => {
    if (!ready || location.hash !== `#${id}` || scrolledFor.current === location.key) return
    scrolledFor.current = location.key
    const target = document.getElementById(id)
    if (!target) return
    const bars = document.querySelectorAll<HTMLElement>('[data-shell-bar]')
    const covered = Math.max(0, ...Array.from(bars, (bar) => bar.offsetHeight))
    const top = target.getBoundingClientRect().top + window.scrollY - covered - 16
    window.scrollTo(0, Math.max(0, top))
  }, [id, ready, location.hash, location.key])
}
