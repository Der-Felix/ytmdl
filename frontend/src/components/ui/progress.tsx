import { Progress as ProgressPrimitive } from "@base-ui/react/progress"

import { cn } from "@/lib/utils"

/**
 * A plain progress bar. `value={null}` renders the indeterminate state, which
 * is what a job whose catalogue is still being resolved actually is — its
 * total is not known yet, so it must not be drawn as 0 %.
 */
function Progress({
  className,
  value,
  ...props
}: ProgressPrimitive.Root.Props) {
  return (
    <ProgressPrimitive.Root value={value} data-slot="progress" {...props}>
      <ProgressPrimitive.Track
        data-slot="progress-track"
        className={cn(
          "relative flex h-1.5 w-full items-center overflow-hidden rounded-full bg-white/8",
          className
        )}
      >
        <ProgressPrimitive.Indicator
          data-slot="progress-indicator"
          className={cn(
            "h-full rounded-full bg-primary transition-[width] duration-500 ease-out",
            // Indeterminate: a soft pulse rather than a bar sliding around.
            "data-indeterminate:w-full data-indeterminate:animate-[progress-pulse_1.8s_ease-in-out_infinite] data-indeterminate:bg-primary/40"
          )}
        />
      </ProgressPrimitive.Track>
    </ProgressPrimitive.Root>
  )
}

export { Progress }
