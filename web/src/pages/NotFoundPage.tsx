/**
 * <NotFoundPage />          inside the AppShell: the empty-state illustration and a
 *                           router link back to Inicio
 * <NotFoundPage public />   signed out, on its own page with the wordmark: the way
 *                           back is the portada (/)
 *
 * A router <Link>, never a plain <a>: going back must not reload the app.
 */
import { Link } from 'react-router-dom'

import { Wordmark } from '@/components/brand'
import { EmptyState } from '@/components/kit'
import { shell } from '@/lib/layout'
import { HOME_PATH } from '@/lib/nav'
import { useDocumentTitle } from '@/lib/title'
import { cn } from '@/lib/utils'

export function NotFoundPage({ public: isPublic = false }: { public?: boolean }) {
  useDocumentTitle('Esta página no existe')
  if (!isPublic) {
    return (
      <EmptyState
        headingLevel={1}
        title="Esta página no existe"
        description="Revisa la dirección o vuelve a Inicio."
        action={{ label: 'Ir a Inicio', to: HOME_PATH }}
        className="md:py-16"
      />
    )
  }

  return (
    <div className="flex min-h-dvh flex-col bg-background">
      <header className={cn(shell, 'flex h-16 items-center')}>
        <Link
          to="/"
          aria-label="chenchén, portada"
          className="flex h-12 items-center rounded-full"
        >
          <Wordmark size="bar" />
        </Link>
      </header>
      <main className={cn(shell, 'flex flex-1 items-center justify-center pb-16')}>
        <EmptyState
          headingLevel={1}
          title="Esta página no existe"
          description="Revisa la dirección o vuelve a la portada."
          action={{ label: 'Ir a la portada', to: '/' }}
        />
      </main>
    </div>
  )
}
