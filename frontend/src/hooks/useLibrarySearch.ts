import { useCallback, useEffect, useRef, useState } from 'react'

import { librarySearch } from '@/lib/api/library'
import type { LibrarySearchResults } from '@/types/api'

export interface LibrarySearchOptions {
  /** Wait this long after the last keystroke before asking the server. */
  debounceMs?: number
  /** Per group; the server caps it at 50. */
  limit?: number
  /** Shorter queries ask nothing and drop the results. */
  minLength?: number
  /** Called once results for a query have arrived (not for stale ones). */
  onResults?: (results: LibrarySearchResults, query: string) => void
}

export interface LibrarySearch {
  /** The latest results; kept while a newer query is on its way. */
  results: LibrarySearchResults | null
  /** The query `results` belong to. */
  resultQuery: string
  /** A request is in flight. */
  isLoading: boolean
  /** The last request failed (not aborted). */
  failed: boolean
  /** Schedules a search for this text; replaces any pending or running one. */
  search: (query: string) => void
  /** Cancels everything and drops the results. */
  clear: () => void
}

/**
 * Live search over the user's library (`GET /library/search`), shared by the
 * library page's field and the command menu.
 *
 * Each call to `search` cancels the timer and aborts the request still
 * running, so only the newest query can ever land; a sequence number guards
 * against a response that slips past the abort. Results stay until newer ones
 * replace them, so a list does not blink empty between keystrokes.
 */
export function useLibrarySearch({
  debounceMs = 150,
  limit = 5,
  minLength = 1,
  onResults,
}: LibrarySearchOptions = {}): LibrarySearch {
  const [results, setResults] = useState<LibrarySearchResults | null>(null)
  const [resultQuery, setResultQuery] = useState('')
  const [isLoading, setIsLoading] = useState(false)
  const [failed, setFailed] = useState(false)

  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const controllerRef = useRef<AbortController | null>(null)
  const seqRef = useRef(0)
  const onResultsRef = useRef(onResults)
  useEffect(() => {
    onResultsRef.current = onResults
  })

  const cancel = useCallback(() => {
    if (timerRef.current) clearTimeout(timerRef.current)
    timerRef.current = null
    controllerRef.current?.abort()
    controllerRef.current = null
    seqRef.current++
  }, [])

  const clear = useCallback(() => {
    cancel()
    setResults(null)
    setResultQuery('')
    setIsLoading(false)
    setFailed(false)
  }, [cancel])

  const run = useCallback(
    async (query: string) => {
      const controller = new AbortController()
      controllerRef.current = controller
      const seq = ++seqRef.current
      const current = () => !controller.signal.aborted && seq === seqRef.current

      setIsLoading(true)
      try {
        const data = await librarySearch(query, { limit, signal: controller.signal })
        if (!current()) return
        setResults(data)
        setResultQuery(query)
        setFailed(false)
        onResultsRef.current?.(data, query)
      } catch {
        if (!current()) return
        setResults(null)
        setResultQuery(query)
        setFailed(true)
      } finally {
        if (current()) setIsLoading(false)
      }
    },
    [limit],
  )

  const search = useCallback(
    (raw: string) => {
      const query = raw.trim()
      cancel()
      if (query.length < minLength) {
        setResults(null)
        setResultQuery('')
        setIsLoading(false)
        setFailed(false)
        return
      }
      timerRef.current = setTimeout(() => {
        timerRef.current = null
        void run(query)
      }, debounceMs)
    },
    [cancel, debounceMs, minLength, run],
  )

  useEffect(() => cancel, [cancel])

  return { results, resultQuery, isLoading, failed, search, clear }
}
