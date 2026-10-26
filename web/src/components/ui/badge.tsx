import * as React from "react"
import { cva, type VariantProps } from "class-variance-authority"
import { Slot } from "radix-ui"

import { cn } from "@/lib/utils"

/**
 * Badge, drawn as the spec's chip: 28px pill, Figtree 14/20 600, 16px icon,
 * padding 0 10 (0 10 0 8 with a leading icon). Not interactive unless asChild
 * wraps a link.
 *
 * Variants:
 *   default / secondary / outline   neutral: surface2, muted text ("Sincronizado",
 *                                    "Sin saldo inicial")
 *   positive    positive-soft, positive text ("Cuadra", "Aplicado")
 *   attention   solid attention fill ("No cuadra · $25.00"). The only orange fill.
 *   destructive older name for attention
 *   accent      primary-soft, primary-text (a filter with a value)
 *   label       micro 12/16 on surface, muted: the type tag on a proposal card
 *   ghost / link
 */
const badgeVariants = cva(
  "group/badge inline-flex h-7 w-fit shrink-0 items-center justify-center gap-1.5 overflow-hidden rounded-full px-2.5 text-small font-semibold whitespace-nowrap transition-colors has-[>svg:first-child]:pl-2 has-data-[icon=inline-start]:pl-2 has-data-[icon=inline-end]:pr-2 [&>svg]:pointer-events-none [&>svg]:size-4!",
  {
    variants: {
      variant: {
        default: "bg-muted text-muted-foreground [a]:hover:bg-line",
        secondary: "bg-muted text-muted-foreground [a]:hover:bg-line",
        outline: "bg-muted text-muted-foreground [a]:hover:bg-line",
        positive: "bg-positive-soft text-positive",
        attention: "bg-attention text-attention-foreground",
        destructive: "bg-attention text-attention-foreground",
        accent: "bg-primary-soft text-primary-text",
        label:
          "h-auto bg-card px-2 py-0.5 text-micro text-muted-foreground has-[>svg:first-child]:pl-2",
        ghost: "bg-transparent text-muted-foreground [a]:hover:bg-muted",
        link: "bg-transparent px-0 text-primary-text underline-offset-4 hover:underline",
      },
    },
    defaultVariants: {
      variant: "default",
    },
  }
)

function Badge({
  className,
  variant = "default",
  asChild = false,
  ...props
}: React.ComponentProps<"span"> &
  VariantProps<typeof badgeVariants> & { asChild?: boolean }) {
  const Comp = asChild ? Slot.Root : "span"

  return (
    <Comp
      data-slot="badge"
      data-variant={variant}
      className={cn(badgeVariants({ variant }), className)}
      {...props}
    />
  )
}

export { Badge, badgeVariants }
