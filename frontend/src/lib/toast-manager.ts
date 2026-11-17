import { Toast } from '@base-ui/react/toast'

/**
 * The application's one toast manager. Adding a toast is a plain function
 * call, so code outside React (the player, API helpers) can report a result
 * without a hook. The Toaster in components/ui/toast renders what it holds;
 * nothing here talks to the server.
 */
export const toast = Toast.createToastManager()
