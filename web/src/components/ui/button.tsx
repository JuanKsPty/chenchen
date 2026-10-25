import * as React from "react"
import { cva, type VariantProps } from "class-variance-authority"
import { Slot } from "radix-ui"

import { cn } from "@/lib/utils"

/**
 * Button. Always a pill, Figtree 16/24 600, icons 20px.
 *
 * Variants:
 *   default      primary veranera fill, white text. One per view.
 *   secondary    surface2 fill, text colour; hover is `line`.
 *   ghost        transparent, primary-text ("Ver todos", "Categorizar"). Padding 0 12.
 *   destructive  inverse fill. Only in a delete confirmation. Never orange.
 *   notice       surface fill, attention text: "Revisar" on the No cuadra notice.
 *   tonal        primary-soft fill, primary-text: the Asistente toggle. With
 *                aria-pressed="true" it turns inverse (panel open).
 *   plain        transparent, text colour, hover surface2: plain icon buttons.
 *   outline      deprecated alias of secondary, kept for old screens.
 *   link         inline text link in primary-text.
 *
 * Sizes:
 *   default 44 (h-11) · bar 40 (desktop top bar, desktop proposals) · lg 48
 *   (full-width mobile CTAs, landing) · icon 44 · icon-bar 40.
 *   sm, xs, icon-sm, icon-xs and icon-lg are older names kept for old screens:
 *   sm/xs/icon-sm = 40, icon-xs = 36, icon-lg = 48.
 *
 * Disabled is surface2 with muted text (no opacity). Focus is the global 2px
 * outline, never a ring.
 */
const buttonVariants = cva(
  "group/button inline-flex shrink-0 items-center justify-center gap-2 rounded-full text-body font-semibold whitespace-nowrap transition-colors duration-180 ease-out select-none active:not-aria-[haspopup]:translate-y-px disabled:translate-y-0 disabled:cursor-not-allowed disabled:bg-muted disabled:text-muted-foreground aria-invalid:ring-2 aria-invalid:ring-destructive [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-5",
  {
    variants: {
      variant: {
        default: "bg-primary text-primary-foreground hover:bg-primary-hover",
        secondary:
          "bg-secondary text-secondary-foreground hover:bg-line aria-expanded:bg-line",
        outline:
          "bg-secondary text-secondary-foreground hover:bg-line aria-expanded:bg-line",
        ghost:
          "bg-transparent text-primary-text hover:bg-muted aria-expanded:bg-muted disabled:bg-transparent",
        destructive:
          "bg-inverse text-inverse-foreground hover:bg-inverse/85",
        notice: "bg-card text-attention hover:bg-muted",
        tonal:
          "bg-primary-soft text-primary-text hover:bg-primary-soft/70 aria-pressed:bg-inverse aria-pressed:text-inverse-foreground aria-pressed:hover:bg-inverse/85",
        plain:
          "bg-transparent text-foreground hover:bg-muted aria-expanded:bg-muted disabled:bg-transparent",
        link: "bg-transparent text-primary-text underline-offset-4 hover:underline disabled:bg-transparent",
      },
      size: {
        default:
          "h-11 px-5 has-[>svg:first-child]:pl-4 has-data-[icon=inline-start]:pl-4 has-data-[icon=inline-end]:pr-4",
        bar: "h-10 px-5 has-[>svg:first-child]:pl-4 has-data-[icon=inline-start]:pl-4 has-data-[icon=inline-end]:pr-4",
        lg: "h-12 px-5 has-[>svg:first-child]:pl-4 has-data-[icon=inline-start]:pl-4 has-data-[icon=inline-end]:pr-4",
        sm: "h-10 px-4 has-[>svg:first-child]:pl-3 has-data-[icon=inline-start]:pl-3 has-data-[icon=inline-end]:pr-3",
        xs: "h-10 px-3",
        icon: "size-11 p-0",
        "icon-bar": "size-10 p-0",
        "icon-xs": "size-9 p-0",
        "icon-sm": "size-10 p-0",
        "icon-lg": "size-12 p-0",
      },
    },
    compoundVariants: [
      {
        variant: "link",
        class: "h-auto px-0 has-[>svg:first-child]:pl-0",
      },
      {
        variant: "ghost",
        size: ["default", "bar", "lg", "sm"],
        class: "px-3 has-[>svg:first-child]:pl-3",
      },
    ],
    defaultVariants: {
      variant: "default",
      size: "default",
    },
  }
)

function Button({
  className,
  variant = "default",
  size = "default",
  asChild = false,
  ...props
}: React.ComponentProps<"button"> &
  VariantProps<typeof buttonVariants> & {
    asChild?: boolean
  }) {
  const Comp = asChild ? Slot.Root : "button"

  return (
    <Comp
      data-slot="button"
      data-variant={variant}
      data-size={size}
      className={cn(buttonVariants({ variant, size, className }))}
      {...props}
    />
  )
}

export { Button, buttonVariants }
