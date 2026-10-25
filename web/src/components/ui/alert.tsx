import * as React from "react"
import { cva, type VariantProps } from "class-variance-authority"

import { cn } from "@/lib/utils"

/**
 * Alert, drawn as the spec's notice: a 16px-radius block with no border,
 * padding 16, a 24px icon in the first column, title in body 600 and the
 * description in small 500 muted. An AlertAction sits in its own last column,
 * vertically centred.
 *
 * Variants:
 *   default      surface2: a pending or neutral notice
 *   attention    attention-soft with the icon in attention: "No cuadra" and
 *                real errors
 *   destructive  older name for attention, kept for old screens
 */
const alertVariants = cva(
  "group/alert relative grid w-full items-center gap-x-3 gap-y-0.5 rounded-2xl p-4 text-left text-body text-foreground has-[>svg]:grid-cols-[1.5rem_1fr] has-data-[slot=alert-action]:grid-cols-[1fr_auto] has-[>svg]:has-data-[slot=alert-action]:grid-cols-[1.5rem_1fr_auto] *:[svg]:row-span-2 *:[svg:not([class*='size-'])]:size-6",
  {
    variants: {
      variant: {
        default: "bg-muted *:[svg]:text-muted-foreground",
        attention: "bg-attention-soft *:[svg]:text-attention",
        destructive: "bg-attention-soft *:[svg]:text-attention",
      },
    },
    defaultVariants: {
      variant: "default",
    },
  }
)

function Alert({
  className,
  variant,
  ...props
}: React.ComponentProps<"div"> & VariantProps<typeof alertVariants>) {
  return (
    <div
      data-slot="alert"
      data-variant={variant ?? "default"}
      role="alert"
      className={cn(alertVariants({ variant }), className)}
      {...props}
    />
  )
}

function AlertTitle({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="alert-title"
      className={cn(
        "font-semibold group-has-[>svg]/alert:col-start-2 [&_a]:underline [&_a]:underline-offset-3",
        className
      )}
      {...props}
    />
  )
}

function AlertDescription({
  className,
  ...props
}: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="alert-description"
      className={cn(
        "text-small font-medium text-pretty text-muted-foreground group-has-[>svg]/alert:col-start-2 [&_a]:underline [&_a]:underline-offset-3 [&_a]:hover:text-foreground [&_p:not(:last-child)]:mb-2",
        className
      )}
      {...props}
    />
  )
}

function AlertAction({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="alert-action"
      className={cn(
        "col-start-2 row-span-2 row-start-1 flex items-center justify-end gap-2 group-has-[>svg]/alert:col-start-3",
        className
      )}
      {...props}
    />
  )
}

export { Alert, AlertTitle, AlertDescription, AlertAction }
