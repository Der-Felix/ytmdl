import { expect, it } from 'bun:test'
import { boundedBlob } from './offline-store'
it('rejects advertised oversized audio before reading it', async () => {
  let canceled = false
  const body = new ReadableStream<Uint8Array>({
    start(controller) {
      controller.enqueue(new Uint8Array([1, 2, 3]))
    },
    cancel() {
      canceled = true
    },
  })
  await expect(
    boundedBlob(
      new Response(body, { headers: { 'Content-Length': '100' } }),
      4,
    ),
  ).rejects.toThrow('Größenlimit')
  expect(canceled).toBe(true)
})
it('bounds streamed bytes without trusting an absent length header', async () => {
  const body = new ReadableStream<Uint8Array>({
    start(c) {
      c.enqueue(new Uint8Array([1, 2, 3]))
      c.enqueue(new Uint8Array([4, 5, 6]))
      c.close()
    },
  })
  await expect(boundedBlob(new Response(body), 4)).rejects.toThrow(
    'Größenlimit',
  )
  const valid = await boundedBlob(
    new Response(new Uint8Array([1, 2, 3]), {
      headers: { 'Content-Type': 'audio/wav' },
    }),
    4,
  )
  expect(valid.size).toBe(3)
  expect(valid.type).toBe('audio/wav')
})
it('cancels a stalled stream and distinguishes expired authentication', async () => {
  const controller = new AbortController()
  const body = new ReadableStream<Uint8Array>({
    start(c) {
      c.enqueue(new Uint8Array([1]))
    },
  })
  const work = boundedBlob(new Response(body), 4, controller.signal)
  controller.abort()
  await expect(work).rejects.toThrow()
  await expect(
    boundedBlob(new Response(null, { status: 401 }), 4),
  ).rejects.toThrow('anmelden')
})
