import * as React from "react"
import { Input as InputPrimitive } from "@base-ui/react/input"

import { cn } from "@/lib/utils"

function Input({ className, type, ...props }: React.ComponentProps<"input">) {
  return (
    <InputPrimitive
      type={type}
      data-slot="input"
      className={cn(
        "h-10 w-full min-w-0 rounded-xl border border-input bg-white/4 px-3.5 text-sm text-foreground transition-colors outline-none",
        "placeholder:text-muted-foreground",
        "hover:border-white/12",
        "focus-visible:border-primary/50 focus-visible:bg-white/6 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring",
        "disabled:cursor-not-allowed disabled:opacity-45",
        "aria-invalid:border-destructive/60",
        className
      )}
      {...props}
    />
  )
}

export { Input }
