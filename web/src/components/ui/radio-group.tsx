"use client"

import * as React from "react"
import { RadioGroup as RadioGroupPrimitive } from "radix-ui"

import { cn } from "@/lib/utils"

/**
 * Radio group (Radix, from the radix-ui package).
 *
 *   <RadioGroup value onValueChange aria-label|aria-labelledby className?>
 *     <label className="flex h-13 items-center gap-3 rounded-full px-3 hover:bg-muted">
 *       <RadioGroupItem value="…" />
 *       …icon and text…
 *     </label>
 *   </RadioGroup>
 *
 * The item is the spec's 22px radio: a 2px muted ring when empty; when checked
 * a veranera disc with an 8px white dot. It is only the control. The tap target
 * is the row around it, which must be at least 44px tall (52 in the
 * Categorizar sheet), so wrap it in a <label>. Arrow keys move the choice.
 */
function RadioGroup({
  className,
  ...props
}: React.ComponentProps<typeof RadioGroupPrimitive.Root>) {
  return (
    <RadioGroupPrimitive.Root
      data-slot="radio-group"
      className={cn("grid gap-1", className)}
      {...props}
    />
  )
}

function RadioGroupItem({
  className,
  ...props
}: React.ComponentProps<typeof RadioGroupPrimitive.Item>) {
  return (
    <RadioGroupPrimitive.Item
      data-slot="radio-group-item"
      className={cn(
        "relative inline-flex size-5.5 shrink-0 cursor-pointer items-center justify-center rounded-full bg-transparent ring-2 ring-muted-foreground transition-colors duration-180 ease-out ring-inset data-checked:bg-primary data-checked:ring-0 disabled:cursor-not-allowed disabled:ring-muted-foreground/50 data-checked:disabled:bg-muted-foreground/50 aria-invalid:ring-destructive",
        className
      )}
      {...props}
    >
      <RadioGroupPrimitive.Indicator
        data-slot="radio-group-indicator"
        className="flex items-center justify-center"
      >
        <span className="size-2 rounded-full bg-primary-foreground" />
      </RadioGroupPrimitive.Indicator>
    </RadioGroupPrimitive.Item>
  )
}

export { RadioGroup, RadioGroupItem }
