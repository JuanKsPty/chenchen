"use client"

import * as React from "react"
import { Switch as SwitchPrimitive } from "radix-ui"

import { cn } from "@/lib/utils"

/**
 * Switch (Radix, from the radix-ui package).
 *
 *   <label className="flex min-h-11 items-center gap-3">
 *     <span id="t">Anotar compras…</span>
 *     <Switch aria-labelledby="t" checked onCheckedChange />
 *   </label>
 *
 * For something that is on or off rather than chosen from a list: a 48×28 pill, the
 * track muted when off and veranera when on, with a white 24px thumb that slides
 * over in 180ms. It is only the control; the tap target is the row around it, at
 * least 44px tall, so wrap it in a <label> and name it by the row's text.
 */
function Switch({
  className,
  ...props
}: React.ComponentProps<typeof SwitchPrimitive.Root>) {
  return (
    <SwitchPrimitive.Root
      data-slot="switch"
      className={cn(
        "peer inline-flex h-7 w-12 shrink-0 cursor-pointer items-center rounded-full bg-muted-foreground p-0.5 transition-colors duration-180 ease-out data-[state=checked]:bg-primary disabled:cursor-not-allowed disabled:bg-muted-foreground/50 data-[state=checked]:disabled:bg-primary/50",
        className
      )}
      {...props}
    >
      <SwitchPrimitive.Thumb
        data-slot="switch-thumb"
        className="pointer-events-none block size-6 rounded-full bg-primary-foreground shadow-card transition-transform duration-180 ease-out data-[state=checked]:translate-x-5"
      />
    </SwitchPrimitive.Root>
  )
}

export { Switch }
