import { Button as ButtonPrimitive } from "@base-ui/react/button"
import { cva, type VariantProps } from "class-variance-authority"

import { cn } from "@/lib/utils"

const buttonVariants = cva(
  "group/button inline-flex shrink-0 items-center justify-center gap-2 rounded-xl border border-transparent text-sm font-medium whitespace-nowrap transition-colors outline-none select-none focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring disabled:pointer-events-none disabled:opacity-45 [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4",
  {
    variants: {
      variant: {
        // The accent surface. Used once per view at most, for the action the
        // view exists for.
        default:
          "bg-primary text-primary-foreground shadow-[0_1px_0_rgb(255_255_255/0.14)_inset] hover:bg-[color-mix(in_oklab,var(--primary),white_12%)] active:bg-[color-mix(in_oklab,var(--primary),black_6%)]",
        // The default for everything else: a panel that reacts.
        outline:
          "border-border bg-white/5 text-foreground hover:border-white/12 hover:bg-white/8",
        secondary:
          "bg-secondary text-secondary-foreground hover:bg-white/10",
        // A tinted accent, for secondary actions that still belong to the
        // accent family. Deliberately low opacity, never solid pink.
        accent:
          "border-primary/25 bg-accent text-accent-foreground hover:bg-[rgb(206_52_99/0.2)]",
        ghost:
          "text-muted-foreground hover:bg-white/6 hover:text-foreground",
        destructive:
          "border-destructive/25 bg-destructive/12 text-destructive hover:bg-destructive/20 focus-visible:outline-destructive",
        link: "text-primary underline-offset-4 hover:underline",
      },
      size: {
        default: "h-10 px-4",
        sm: "h-8 gap-1.5 rounded-lg px-3 text-[0.8125rem]",
        lg: "h-11 px-5 text-[0.9375rem]",
        icon: "size-10",
        "icon-sm": "size-8 rounded-lg",
      },
    },
    defaultVariants: {
      variant: "outline",
      size: "default",
    },
  }
)

function Button({
  className,
  variant,
  size,
  ...props
}: ButtonPrimitive.Props & VariantProps<typeof buttonVariants>) {
  return (
    <ButtonPrimitive
      data-slot="button"
      className={cn(buttonVariants({ variant, size, className }))}
      {...props}
    />
  )
}

export { Button, buttonVariants }
