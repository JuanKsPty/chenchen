import * as React from "react"

import { cn } from "@/lib/utils"

/** Multi-line field: surface2, 16px radius (it is a container, not a pill), no border. */
function Textarea({ className, ...props }: React.ComponentProps<"textarea">) {
  return (
    <textarea
      data-slot="textarea"
      className={cn(
        "flex field-sizing-content min-h-24 w-full rounded-2xl bg-input px-4 py-3 text-body text-foreground transition-colors placeholder:text-muted-foreground disabled:cursor-not-allowed disabled:text-muted-foreground aria-invalid:ring-2 aria-invalid:ring-destructive",
        className
      )}
      {...props}
    />
  )
}

export { Textarea }
