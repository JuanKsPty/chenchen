import { CircleCheckIcon, WalletIcon } from 'lucide-react'
import type { ReactNode } from 'react'
import { Link } from 'react-router-dom'

import { AssistantIcon, heroImage, Illustration, Wordmark } from '@/components/brand'
import { Button } from '@/components/ui/button'
import { shell } from '@/lib/layout'
import { cn } from '@/lib/utils'
import { PublicHeader } from './AuthLayout'

/**
 * The portada (spec landingHero, artboard C-Portada-Desktop).
 *
 * Above the fold only the offer: the 72px bar with the wordmark and nothing else,
 * then the headline, the lede and the two ways in beside the hero collage (5fr /
 * 7fr). Below it, what chenchén does in three short lines, what it reads, and a
 * footer that says plainly it is not a bank.
 *
 * The two columns start at lg rather than md: between 768 and 1023 the 5fr column
 * would be about 270px, too narrow for the 48px headline, so tablets get the
 * stacked layout with the desktop type. Below md the CTAs go full width.
 */
export function LandingPage() {
  return (
    <div className="flex min-h-dvh flex-col bg-background">
      <PublicHeader />

      <main className="flex-1">
        <section
          aria-labelledby="portada-titulo"
          className={cn(
            shell,
            'grid items-center gap-x-14 gap-y-10 pt-6 pb-12 md:py-16 lg:min-h-140 lg:grid-cols-[5fr_7fr]',
          )}
        >
          <div className="flex min-w-0 flex-col">
            <h1
              id="portada-titulo"
              className="font-display text-title font-extrabold text-balance md:max-w-[12ch] md:text-display"
            >
              Cuánto tienes, cuánto debes y en qué se te va.
            </h1>
            <p className="mt-4 text-body font-medium text-pretty text-muted-foreground md:max-w-[34ch] md:text-heading">
              Sube los estados de cuenta de tu banco y chenchén te dice, al centavo, si cada
              cuenta cuadra. No movemos tu plata: solo la ordenamos.
            </p>
            <div className="mt-8 flex flex-col gap-3 md:flex-row">
              <Button asChild size="lg" className="w-full md:w-auto md:px-7">
                <Link to="/entrar">Entrar</Link>
              </Button>
              <Button
                asChild
                size="lg"
                variant="secondary"
                className="w-full md:w-auto md:px-6"
              >
                <Link to="/registro">Crear cuenta</Link>
              </Button>
            </div>
          </div>

          <img
            src={heroImage.src}
            srcSet={heroImage.srcSet}
            sizes="(min-width: 1184px) 621px, (min-width: 1024px) 53vw, 100vw"
            width={heroImage.width}
            height={heroImage.height}
            alt="Collage de papel con brácteas de veranera magenta y rosa, monedas mango y una barrita negra inclinada"
            // The largest paint above the fold: fetched first and decoded with
            // the frame rather than after it (no decoding="async").
            fetchPriority="high"
            className="block aspect-3/2 w-full min-w-0 rounded-2xl object-cover"
          />
        </section>

        <section aria-label="Qué hace chenchén" className={cn(shell, 'pt-4 pb-16 md:pt-8')}>
          <div className="grid gap-10 md:grid-cols-3 md:gap-8 lg:gap-10">
            <Feature icon={<WalletIcon />} title="Cuánto tienes y cuánto debes">
              Tus cuentas, tu tarjeta y tus inversiones, sumadas en una sola cifra.
            </Feature>
            <Feature icon={<CircleCheckIcon />} title="Si cada cuenta cuadra">
              Comparamos tu saldo con el del banco. Si no cuadra, te decimos cuánto y desde
              cuándo.
            </Feature>
            <Feature
              icon={<AssistantIcon variant="mobile" />}
              title="Un asistente que propone"
            >
              Analiza tus movimientos y te sugiere cambios. Tú decides qué aplicar.
            </Feature>
          </div>
        </section>

        <section className={cn(shell, 'pb-16')}>
          <div className="flex flex-col items-start gap-6 rounded-2xl bg-muted p-5 md:flex-row md:items-center md:gap-12 md:p-8">
            <Illustration name="import" className="w-48 md:w-60" />
            <p className="max-w-[28ch] font-display text-heading font-bold text-balance md:text-title">
              Lee los estados de cuenta que ya te da tu banco y se conecta a Interactive
              Brokers.
            </p>
          </div>
        </section>
      </main>

      <footer
        className={cn(
          shell,
          'flex flex-wrap items-center justify-between gap-x-6 gap-y-3 pt-6 pb-10',
        )}
      >
        <div className="flex flex-wrap items-center gap-x-4 gap-y-2">
          <Wordmark size="mobile" />
          <p className="text-small font-medium text-muted-foreground">
            Tu chenchén, al centavo.
          </p>
        </div>
        <p className="text-small font-medium text-muted-foreground">
          chenchén no es un banco: no mueve tu plata.
        </p>
      </footer>
    </div>
  )
}

function Feature({
  icon,
  title,
  children,
}: {
  icon: ReactNode
  title: string
  children: ReactNode
}) {
  return (
    <div className="flex flex-col gap-3">
      <span
        aria-hidden="true"
        className="flex size-12 items-center justify-center rounded-full bg-primary-soft text-primary-text [&_svg]:size-6"
      >
        {icon}
      </span>
      <h2 className="mt-1 font-display text-heading font-bold text-balance">{title}</h2>
      <p className="max-w-[36ch] text-body text-pretty text-muted-foreground">{children}</p>
    </div>
  )
}
