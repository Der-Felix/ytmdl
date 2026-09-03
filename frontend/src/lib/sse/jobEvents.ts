/**
 * The live connection to GET /api/v1/events.
 *
 * One EventSource is shared by the whole application: several views want job
 * progress at the same time, and each of them opening its own stream would
 * cost the backend a subscriber per view. The connection opens with the first
 * subscriber and closes with the last.
 *
 * Reconnecting is left to EventSource itself — the backend sends "retry: 3000"
 * as the first line of the stream, so the browser handles the backoff. What
 * this module adds is a readable connection state, because the UI has to keep
 * working while the stream is down.
 */

import { apiUrl } from '@/lib/api/client'
import { STREAM_EVENT_TYPES } from '@/types/api'
import type { JobEvent } from '@/types/api'

export type ConnectionState = 'connecting' | 'open' | 'offline'

type EventHandler = (event: JobEvent) => void
type StateHandler = (state: ConnectionState) => void

const eventHandlers = new Set<EventHandler>()
const stateHandlers = new Set<StateHandler>()

let source: EventSource | null = null
let state: ConnectionState = 'offline'

/** The current connection state. */
export function connectionState(): ConnectionState {
  return state
}

function setState(next: ConnectionState): void {
  if (state === next) return
  state = next
  for (const handler of stateHandlers) handler(next)
}

function open(): void {
  if (source) return

  setState('connecting')
  const stream = new EventSource(apiUrl('/events'))
  source = stream

  stream.onopen = () => {
    // A late open() from a stream that was already replaced must not report a
    // connection the application no longer holds.
    if (source === stream) setState('open')
  }

  stream.onerror = () => {
    if (source !== stream) return
    // EventSource reconnects on its own unless it closed permanently.
    setState(stream.readyState === EventSource.CLOSED ? 'offline' : 'connecting')
  }

  // The backend names every message, so each type is registered explicitly
  // rather than relying on a default "message" event that never arrives.
  for (const type of STREAM_EVENT_TYPES) {
    stream.addEventListener(type, (message) => {
      if (source !== stream) return
      const event = parse(message)
      if (event) dispatch(event)
    })
  }
}

function close(): void {
  source?.close()
  source = null
  setState('offline')
}

function parse(message: MessageEvent<string>): JobEvent | null {
  try {
    return JSON.parse(message.data) as JobEvent
  } catch {
    // A malformed event is dropped: it must never take the stream down.
    return null
  }
}

function dispatch(event: JobEvent): void {
  for (const handler of eventHandlers) handler(event)
}

/**
 * Registers a job event handler and returns the unsubscribe function. The
 * stream opens with the first subscriber and closes when the last one leaves.
 */
export function subscribeToJobEvents(handler: EventHandler): () => void {
  eventHandlers.add(handler)
  open()

  return () => {
    eventHandlers.delete(handler)
    if (eventHandlers.size === 0) close()
  }
}

/** Registers a connection state handler and reports the current state at once. */
export function subscribeToConnectionState(handler: StateHandler): () => void {
  stateHandlers.add(handler)
  return () => {
    stateHandlers.delete(handler)
  }
}
