/**
 * What this device has already shown of the month's plan alerts, so Inicio speaks
 * only when something rises, not every time it opens.
 *
 *   alertSubject(alert)          what an alert is about: 'libre', 'tope:<category>',
 *                                'pago:<item>' or 'tarjeta:<account>:<due day>'
 *   useUnseenPlanAlerts(plan?)   the subjects whose level is above the one last seen
 *                                ("near" after nothing, "over" after "near")
 *   markPlanSeen(plan)           every current alert's level, as seen now
 *
 * Kept in localStorage under `chenchen:plan-avisos` as { month, seen: { subject:
 * level } }. Marking replaces the record with the month as it stands, so a cap that
 * fell back under 80% and climbs again is news again. A record for another month is
 * no record at all: a new month starts with nothing seen. The month is always the
 * server's (Panama time), never the browser's.
 *
 * Seen is per device, a deliberate choice: there is no channel that tells the owner
 * anything on its own, and what Inicio says is only a pointer to the plan. When
 * storage throws (private browsing, storage disabled, full), what was seen lives
 * for this visit only, in memory, and nothing fails.
 *
 * A card's subject carries its due day: a new statement is a new payment, and says
 * so again. A fixed payment is due once a month, so its item is enough.
 */
import { useMemo, useSyncExternalStore } from 'react'

import type { MonthPlan, PlanAlert } from '@/api/types'

const KEY = 'chenchen:plan-avisos'

type Level = PlanAlert['level']

/** How far an alert has gone: a payment due soon is news once, like "near". */
const RANK: Record<Level, number> = { near: 1, soon: 1, over: 2 }

export function alertSubject(alert: PlanAlert): string {
  switch (alert.kind) {
    case 'free':
      return 'libre'
    case 'cap':
      return `tope:${alert.category_id}`
    case 'payment':
      return 'item_id' in alert
        ? `pago:${alert.item_id}`
        : `tarjeta:${alert.account_id}:${alert.due_on}`
  }
}

// The record as this visit last wrote it, and whether localStorage still works:
// once it throws, the memory copy is the record until the page reloads.
let memory: string | null = null
let storageWorks = true
const listeners = new Set<() => void>()

function readRecord(): string | null {
  if (storageWorks) {
    try {
      return window.localStorage.getItem(KEY)
    } catch {
      storageWorks = false
    }
  }
  return memory
}

function writeRecord(raw: string) {
  memory = raw
  if (storageWorks) {
    try {
      window.localStorage.setItem(KEY, raw)
    } catch {
      storageWorks = false
    }
  }
  for (const listener of listeners) listener()
}

/** Hears this tab's marks, and another tab's through the storage event. */
function subscribe(listener: () => void) {
  listeners.add(listener)
  const onStorage = (event: StorageEvent) => {
    if (event.key === KEY || event.key === null) listener()
  }
  window.addEventListener('storage', onStorage)
  return () => {
    listeners.delete(listener)
    window.removeEventListener('storage', onStorage)
  }
}

/** The levels seen in `month`; nothing for a record of another month, or a broken one. */
function seenIn(raw: string | null, month: string): Map<string, Level> {
  const seen = new Map<string, Level>()
  if (!raw) return seen
  let value: unknown
  try {
    value = JSON.parse(raw)
  } catch {
    return seen
  }
  if (typeof value !== 'object' || value === null) return seen
  const record = value as { month?: unknown; seen?: unknown }
  if (record.month !== month || typeof record.seen !== 'object' || !record.seen) return seen
  for (const [subject, level] of Object.entries(record.seen)) {
    if (level === 'near' || level === 'over' || level === 'soon') seen.set(subject, level)
  }
  return seen
}

export function useUnseenPlanAlerts(plan: MonthPlan | undefined): Set<string> {
  const raw = useSyncExternalStore(subscribe, readRecord)
  return useMemo(() => {
    const unseen = new Set<string>()
    if (!plan) return unseen
    const seen = seenIn(raw, plan.month)
    for (const alert of plan.alerts) {
      const subject = alertSubject(alert)
      const before = seen.get(subject)
      if (RANK[alert.level] > (before ? RANK[before] : 0)) unseen.add(subject)
    }
    return unseen
  }, [raw, plan])
}

/** Records every alert of `plan` at its current level, and nothing else, as seen. */
export function markPlanSeen(plan: Pick<MonthPlan, 'month' | 'alerts'>) {
  const seen: Record<string, Level> = {}
  for (const alert of plan.alerts) seen[alertSubject(alert)] = alert.level
  const raw = JSON.stringify({ month: plan.month, seen })
  if (raw !== readRecord()) writeRecord(raw)
}
