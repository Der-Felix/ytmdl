import { afterEach, beforeEach, describe, expect, it } from 'bun:test'

import { ApiError, request } from './client'

interface Call {
  token: string | null
  body: unknown
}

function csrfError(): Response {
  return new Response(
    JSON.stringify({ error: { code: 'CSRF_INVALID', message: 'CSRF token mismatch' } }),
    { status: 403, headers: { 'Content-Type': 'application/json' } },
  )
}

function ok(): Response {
  return new Response(JSON.stringify({ data: { done: true } }), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  })
}

describe('request CSRF retry', () => {
  let originalFetch: typeof fetch
  let calls: Call[]

  function install(responses: Array<() => Response>, onCall?: (n: number) => void) {
    calls = []
    globalThis.fetch = (async (_url: RequestInfo | URL, init?: RequestInit) => {
      const headers = init?.headers as Record<string, string>
      calls.push({ token: headers['X-CSRF-Token'] ?? null, body: init?.body })
      onCall?.(calls.length)
      return responses[calls.length - 1]!()
    }) as typeof fetch
  }

  beforeEach(() => {
    originalFetch = globalThis.fetch
    ;(window as any).happyDOM.setURL('http://localhost/')
    document.cookie = 'ytmdl_csrf=old; path=/'
  })

  afterEach(() => {
    globalThis.fetch = originalFetch
    document.cookie = 'ytmdl_csrf=; path=/; expires=Thu, 01 Jan 1970 00:00:00 GMT'
  })

  it('retries a mutating request once with the current cookie after CSRF_INVALID', async () => {
    install([csrfError, ok], (n) => {
      // The late response of another request replaces the cookie.
      if (n === 1) document.cookie = 'ytmdl_csrf=new; path=/'
    })

    const data = await request<{ done: boolean }>('/auth/login', {
      method: 'POST',
      body: { username: 'a' },
    })

    expect(data.done).toBe(true)
    expect(calls.map((c) => c.token)).toEqual(['old', 'new'])
    expect(calls[1]!.body).toBe(calls[0]!.body)
  })

  it('returns a second CSRF_INVALID to the caller without a third attempt', async () => {
    install([csrfError, csrfError, ok])

    const error = await request('/auth/login', { method: 'POST', body: {} }).catch(
      (e: unknown) => e,
    )

    expect(error).toBeInstanceOf(ApiError)
    expect((error as ApiError).code).toBe('CSRF_INVALID')
    expect(calls).toHaveLength(2)
  })

  it('does not retry other 403 answers or GET requests', async () => {
    install([
      () =>
        new Response(JSON.stringify({ error: { code: 'FORBIDDEN', message: 'no' } }), {
          status: 403,
        }),
    ])
    await request('/x', { method: 'POST', body: {} }).catch(() => {})
    expect(calls).toHaveLength(1)

    install([csrfError])
    await request('/x').catch(() => {})
    expect(calls).toHaveLength(1)
  })
})
