/**
 * <AppearanceSection className? />
 *
 * Ajustes › Apariencia: Claro, Oscuro or Sistema, as a segmented control.
 *
 * It is a radio group (one choice out of three, arrow keys move it), drawn like the
 * app's tab track: a surface2 pill holding three pills, the chosen one on the surface
 * with the card shadow. Below lg (the mobile chrome, tablets included) it is 52px
 * tall with 44px options; from lg, 48 with 40. Full width on a phone, as wide as
 * its options from md. When the card is narrower than 20rem (phones of 360px and
 * less) the icons step aside so the three words fit.
 * The choice applies at once through `useTheme().setTheme` and is remembered on
 * this device.
 */
import { MonitorSmartphoneIcon, MoonIcon, SunIcon, type LucideIcon } from 'lucide-react'
import { useId } from 'react'
import { RadioGroup as RadioGroupPrimitive } from 'radix-ui'

import { SectionCard } from '@/components/kit'
import { THEME_LABEL, useTheme, type Theme } from '@/lib/theme'
import { cn } from '@/lib/utils'

const OPTIONS: { value: Theme; icon: LucideIcon }[] = [
  { value: 'light', icon: SunIcon },
  { value: 'dark', icon: MoonIcon },
  { value: 'system', icon: MonitorSmartphoneIcon },
]

function isTheme(value: string): value is Theme {
  return value === 'light' || value === 'dark' || value === 'system'
}

export function AppearanceSection({ className }: { className?: string }) {
  const { theme, setTheme } = useTheme()
  const hintId = useId()

  return (
    <SectionCard title="Apariencia" className={className}>
      <div className="@container">
        <RadioGroupPrimitive.Root
          value={theme}
          onValueChange={(value) => {
            if (isTheme(value)) setTheme(value)
          }}
          aria-label="Modo de color"
          aria-describedby={hintId}
          className="grid h-13 w-full grid-cols-3 gap-1 rounded-full bg-muted p-1 md:w-fit lg:h-12"
        >
          {OPTIONS.map(({ value, icon: Icon }) => (
            <RadioGroupPrimitive.Item
              key={value}
              value={value}
              className={cn(
                'inline-flex h-11 min-w-0 cursor-pointer items-center justify-center gap-1.5 rounded-full px-2 text-body font-semibold text-muted-foreground transition-colors duration-180 ease-out hover:text-foreground md:gap-2 md:px-4.5 lg:h-10',
                'data-[state=checked]:bg-card data-[state=checked]:text-foreground data-[state=checked]:shadow-card',
              )}
            >
              <Icon aria-hidden="true" className="size-5 shrink-0 @max-xs:hidden" />
              {THEME_LABEL[value]}
            </RadioGroupPrimitive.Item>
          ))}
        </RadioGroupPrimitive.Root>
      </div>
      <p id={hintId} className="mt-3 text-small font-medium text-muted-foreground">
        Sistema sigue el modo de tu teléfono o tu computadora.
      </p>
    </SectionCard>
  )
}
