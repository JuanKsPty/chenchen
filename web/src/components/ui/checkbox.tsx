"use client"

import * as React from "react"
import { Checkbox as CheckboxPrimitive } from "radix-ui"
import { CheckIcon, MinusIcon } from "lucide-react"

import { cn } from "@/lib/utils"

/**
 * Checkbox (Radix, from the radix-ui package).
 *
 *   <label className="flex min-h-11 items-center gap-3">
 *     <Checkbox checked onCheckedChange />
 *     Hacer lo mismo con …
 *   </label>
 *
 * A 22px rounded square, so it never reads as one more radio in a list of them
 * (Categorizar's "Es una transferencia…" under the categories): a 2px muted ring
 * when empty; when checked a veranera fill with a white check (a minus when
 * indeterminate). Its 6px corner is the one exception to the 16px radius: the spec
 * has no small radius, and 16px on a 22px box is a circle. The tap target is the
 * <label> around it, which must be at least 44px tall and may wrap to two lines.
 */
function Checkbox({
  className,
  ...props
}: React.ComponentProps<typeof CheckboxPrimitive.Root>) {
  return (
    <CheckboxPrimitive.Root
      data-slot="checkbox"
      className={cn(
        "peer relative inline-flex size-5.5 shrink-0 cursor-pointer items-center justify-center rounded-[0.375rem] bg-transparent text-primary-foreground ring-2 ring-muted-foreground transition-colors duration-180 ease-out ring-inset data-checked:bg-primary data-checked:ring-0 data-[state=indeterminate]:bg-primary data-[state=indeterminate]:ring-0 disabled:cursor-not-allowed disabled:ring-muted-foreground/50 data-checked:disabled:bg-muted-foreground/50 aria-invalid:ring-destructive",
        className
      )}
      {...props}
    >
      <CheckboxPrimitive.Indicator
        data-slot="checkbox-indicator"
        className="group/checkbox-indicator flex items-center justify-center"
      >
        <CheckIcon
          strokeWidth={3}
          className="size-3.5 group-data-[state=indeterminate]/checkbox-indicator:hidden"
        />
        <MinusIcon
          strokeWidth={3}
          className="hidden size-3.5 group-data-[state=indeterminate]/checkbox-indicator:block"
        />
      </CheckboxPrimitive.Indicator>
    </CheckboxPrimitive.Root>
  )
}

export { Checkbox }
