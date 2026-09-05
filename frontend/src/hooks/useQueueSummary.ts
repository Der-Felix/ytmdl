import { useCallback, useEffect, useRef } from 'react'

import { getQueueSummary } from '@/lib/api/jobs'
import { useAsync } from '@/hooks/useAsync'
import type { AsyncState } from '@/hooks/useAsync'
import { useConnectionState, useJobEvents } from '@/hooks/useJobs'
import type { JobEvent, QueueSummary } from '@/types/api'

export interface UseQueueSummaryResult {
  state: AsyncState<QueueSummary>
  reload: () => void
}

/**
 * Loads the queue summary (ETA, throughput, active workers, next-up jobs)
 * and keeps it live via SSE without polling.
 */
export function useQueueSummary(): UseQueueSummaryResult {
  const { state, reload, setData } = useAsync(
    (signal) => getQueueSummary(signal),
    [],
  )

  const connection = useConnectionState()
  const debounceTimer = useRef<ReturnType<typeof setTimeout> | null>(null)

  const scheduleReload = useCallback(() => {
    if (debounceTimer.current) clearTimeout(debounceTimer.current)
    debounceTimer.current = setTimeout(() => {
      reload()
    }, 500)
  }, [reload])

  // Cleanup timer on unmount
  useEffect(() => {
    return () => {
      if (debounceTimer.current) clearTimeout(debounceTimer.current)
    }
  }, [])

  useJobEvents(
    useCallback(
      (event: JobEvent) => {
        // Fast path: live update download percentage for active workers
        if (event.type === 'job.progress' && event.item_id !== undefined && event.download_percent !== undefined) {
          setData((prev) => {
            if (!prev || !prev.current) return prev
            const nextCurrent = prev.current.map((worker) => {
              if (worker.item_id === event.item_id) {
                return { ...worker, progress_percent: event.download_percent ?? worker.progress_percent }
              }
              return worker
            })
            return { ...prev, current: nextCurrent }
          })
          return
        }

        // Structural queue changes trigger debounced summary refetch
        switch (event.type) {
          case 'job.created':
          case 'job.status':
          case 'job.completed':
          case 'job.failed':
          case 'job.cancelled':
          case 'job.paused':
          case 'job.resumed':
          case 'job.retried':
          case 'job.item':
            scheduleReload()
            break
        }
      },
      [scheduleReload, setData],
    ),
  )

  // Reload when connection reopens
  const wasOffline = useRef(false)
  useEffect(() => {
    if (connection === 'open' && wasOffline.current) {
      reload()
    }
    wasOffline.current = connection !== 'open'
  }, [connection, reload])

  return { state, reload }
}
