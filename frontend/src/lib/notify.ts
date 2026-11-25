import { toast } from '@/lib/toast-manager'

/**
 * Results and failures of user actions, shown as toasts. Form validation stays
 * at its field; this is for what happens after an action was sent: a title
 * queued, a favourite saved, a request that failed without a form to show it.
 */

export function notifySuccess(title: string, description?: string) {
  return toast.add({ title, description, type: 'success' })
}

export function notifyInfo(title: string, description?: string) {
  return toast.add({ title, description, type: 'info' })
}

/**
 * A result with one follow-up the user may want right away: undo it, open
 * what was made. Stays a little longer than a plain result so the action can
 * be reached; the toast closes itself once the action ran.
 */
export function notifyWithAction(
  title: string,
  action: { label: string; onClick: () => void },
  options: { description?: string; type?: 'success' | 'info'; timeout?: number } = {},
) {
  const id: string = toast.add({
    title,
    description: options.description,
    type: options.type ?? 'success',
    timeout: options.timeout ?? 8000,
    actionProps: {
      children: action.label,
      onClick: () => {
        action.onClick()
        toast.close(id)
      },
    },
  })
  return id
}

/** The description is the error's own message when it has one. */
export function notifyError(title: string, error?: unknown) {
  const description =
    error instanceof Error && error.message
      ? error.message
      : typeof error === 'string' && error
        ? error
        : undefined
  // High priority makes screen readers announce the failure at once.
  return toast.add({ title, description, type: 'error', priority: 'high', timeout: 8000 })
}
