import { useEffect } from 'react'

/**
 * useDocumentTitle(title?)
 *
 * Names the tab after the screen while it is mounted ("Movimientos · chenchén") and
 * puts the plain name back on the way out, so the next screen does not inherit it.
 * Every page calls it once with its own name: the section (Inicio, Cuentas,
 * Movimientos, Gastos, Reglas, Ajustes), the account's short label on a cuenta,
 * Entrar and Crear cuenta on the forms. Without a title (still loading) the tab
 * keeps the plain "chenchén".
 */
export function useDocumentTitle(title?: string) {
  useEffect(() => {
    document.title = title ? `${title} · chenchén` : 'chenchén'
    return () => {
      document.title = 'chenchén'
    }
  }, [title])
}
