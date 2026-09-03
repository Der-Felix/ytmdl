/**
 * The one place data loading is handled. Every data-driven view needs the same
 * four states — loading, success, empty, error — and the same cancellation on
 * unmount, so it lives here instead of in each page.
 */

import { useCallback, useEffect, useRef, useState } from 'react'

import { isAbortError } from '@/lib/api/client'

export type AsyncState<T> =
  | { status: 'loading'; data?: undefined; error?: undefined }
  | { status: 'success'; data: T; error?: undefined }
  | { status: 'error'; data?: undefined; error: unknown }

export interface AsyncResult<T> {
  state: AsyncState<T>
  /** Runs the loader again, e.g. from a retry button. */
  reload: () => void
  /** Replaces the loaded value without a request — used by live updates. */
  setData: (update: T | ((current: T) => T)) => void
}

/**
 * Runs an async loader and exposes its state.
 *
 * The loader receives an AbortSignal and must pass it on, so that a view left
 * mid-request does not settle into unmounted state. `deps` decides when the
 * loader runs again; it is compared the way React compares effect deps.
 */
export function useAsync<T>(
  loader: (signal: AbortSignal) => Promise<T>,
  deps: readonly unknown[],
): AsyncResult<T> {
  const [state, setState] = useState<AsyncState<T>>({ status: 'loading' })
  const [attempt, setAttempt] = useState(0)

  // The loader is usually an inline arrow function and would otherwise
  // re-trigger the effect on every render; deps decide when to reload.
  const loaderRef = useRef(loader)
  useEffect(() => {
    loaderRef.current = loader
  })

  useEffect(() => {
    const controller = new AbortController()
    let active = true

    setState({ status: 'loading' })

    loaderRef
      .current(controller.signal)
      .then((data) => {
        if (active) setState({ status: 'success', data })
      })
      .catch((error: unknown) => {
        // An abort is the expected outcome of navigating away, not a failure
        // to show the user.
        if (active && !isAbortError(error)) setState({ status: 'error', error })
      })

    return () => {
      active = false
      controller.abort()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [...deps, attempt])

  const reload = useCallback(() => setAttempt((value) => value + 1), [])

  const setData = useCallback((update: T | ((current: T) => T)) => {
    setState((current) => {
      if (current.status !== 'success') return current
      const next =
        typeof update === 'function'
          ? (update as (value: T) => T)(current.data)
          : update
      return { status: 'success', data: next }
    })
  }, [])

  return { state, reload, setData }
}
