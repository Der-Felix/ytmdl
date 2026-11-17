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
