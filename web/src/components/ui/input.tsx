import * as React from "react"

import { cn } from "@/lib/utils"

/**
 * Text input: a 48px pill on mobile and 44px from md, surface2 fill, no border,
 * Figtree 16/24 (16px also keeps iOS from zooming in). Focus is the global 2px
 * outline. aria-invalid adds a 2px attention ring.
 */
function Input({ className, type, ...props }: React.ComponentProps<"input">) {
  return (
    <input
      type={type}
      data-slot="input"
      className={cn(
        "h-12 w-full min-w-0 rounded-full bg-input px-4 py-2 text-body text-foreground transition-colors file:mr-3 file:inline-flex file:h-7 file:items-center file:rounded-full file:border-0 file:bg-card file:px-3 file:text-small file:font-semibold file:text-foreground placeholder:text-muted-foreground disabled:cursor-not-allowed disabled:text-muted-foreground aria-invalid:ring-2 aria-invalid:ring-destructive md:h-11",
        className
      )}
      {...props}
    />
  )
}

export { Input }
