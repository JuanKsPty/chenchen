/**
 * What a page tells the mobile top bar.
 *
 * Frozen interface (F6):
 *
 *   useTopBar({ title?, back?, action?, menu? })
 *
 *   title   replaces the bar's title. Root sections already show their own name
 *           (Cuentas, Movimientos, Gastos; "Buenas, Juan" on Inicio), so only a
 *           screen you navigated into needs one: the account's shortAccountLabel,
 *           "Reglas". Truncates with an ellipsis.
 *   back    a path. Shows the 44px chevron "Volver" linking there, before the title.
 *           Never on a root section.
 *   action  a ReactNode at the right, before the Asistente button: a compact 44px
 *           primary (`<Button className="pr-4 pl-3">`, icon 20). Cuentas gets
 *           "Importar" without asking; pass `null` to remove it.
 *   menu    the overflow menu's items: <DropdownMenuItem>s and
 *           <DropdownMenuSeparator>s. The avatar becomes a 44px ellipsis button
 *           ("Más opciones") that opens them. The menu is non-modal, so an item can
 *           open a dialog from its onSelect.
 *
 * Below lg only, where the mobile bar shows: the desktop bar never changes per page;
 * a page draws its own title row there (PageHeader, visible from lg). The settings
 * last while the calling component is mounted, and the bar falls back to the route's
 * defaults when it unmounts.
 *
 *   useTopBar({ title: shortAccountLabel(account), back: '/cuentas', menu: <>…</> })
 */
import {
  createContext,
  useCallback,
  useContext,
  useLayoutEffect,
  useState,
  type ReactNode,
} from 'react'

export interface TopBarConfig {
  title?: string
  back?: string
  /** `undefined` keeps the route's default action; `null` removes it. */
  action?: ReactNode
  menu?: ReactNode
}

type SetTopBar = (owner: symbol, config: TopBarConfig | null) => void

const SetTopBarContext = createContext<SetTopBar | null>(null)
const TopBarStateContext = createContext<TopBarConfig | null>(null)

export function TopBarProvider({ children }: { children: ReactNode }) {
  const [current, setCurrent] = useState<{ owner: symbol; config: TopBarConfig } | null>(
    null,
  )

  // Last caller wins; a caller only clears what it set itself, so a page unmounting
  // after the next one mounted cannot wipe the newcomer's title.
  const set = useCallback<SetTopBar>((owner, config) => {
    setCurrent((previous) => {
      if (config) return { owner, config }
      return previous?.owner === owner ? null : previous
    })
  }, [])

  return (
    <SetTopBarContext.Provider value={set}>
      <TopBarStateContext.Provider value={current?.config ?? null}>
        {children}
      </TopBarStateContext.Provider>
    </SetTopBarContext.Provider>
  )
}

export function useTopBar(config: TopBarConfig): void {
  const set = useContext(SetTopBarContext)
  if (!set) throw new Error('useTopBar must be used inside the AppShell')
  const [owner] = useState(() => Symbol('top-bar'))
  const { title, back, action, menu } = config

  // Layout effects, so the bar never paints the route's default title for a frame
  // before the page's own.
  useLayoutEffect(() => {
    set(owner, { title, back, action, menu })
  }, [set, owner, title, back, action, menu])

  useLayoutEffect(() => () => set(owner, null), [set, owner])
}

/** The current page's settings, for the mobile bar. */
export function useTopBarConfig(): TopBarConfig | null {
  return useContext(TopBarStateContext)
}
