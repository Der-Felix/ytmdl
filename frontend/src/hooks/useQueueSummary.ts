import { useSyncExternalStore } from 'react'

import { queueSummaryStore } from '@/lib/queueSummaryStore'
import type { AsyncState } from '@/hooks/useAsync'
import type { QueueSummary } from '@/types/api'

export interface UseQueueSummaryResult {
  state: AsyncState<QueueSummary>
  /** True while a reload requested through `reload` is in flight. */
  refreshing: boolean
  reload: () => void
}

/**
 * The queue summary (ETA, throughput, active workers, next-up jobs), kept live
 * via SSE without polling. All mounted views share one store and therefore one
 * request; see queueSummaryStore for the throttling rules.
 */
export function useQueueSummary(): UseQueueSummaryResult {
  const snapshot = useSyncExternalStore(
    queueSummaryStore.subscribe,
    queueSummaryStore.getSnapshot,
    queueSummaryStore.getSnapshot,
  )
  return { state: snapshot.state, refreshing: snapshot.refreshing, reload: queueSummaryStore.reload }
}
