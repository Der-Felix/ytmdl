import { afterEach, beforeEach, describe, expect, it } from 'bun:test'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'

import { SubscriptionImportDialog } from './SubscriptionImportDialog'
import type { ImportPreview, ImportResult, SubscriptionExport } from '@/types/api'

let originalFetch: typeof fetch

beforeEach(() => {
  originalFetch = globalThis.fetch
})

afterEach(() => {
  globalThis.fetch = originalFetch
})

describe('SubscriptionImportDialog', () => {
  const sampleExport: SubscriptionExport = {
    format: 'ytmdl-subscriptions',
    version: 1,
    exported_at: new Date().toISOString(),
    subscriptions: [
      {
        artist_name: 'Daft Punk',
        provider: 'deezer',
        artist_source_id: '27',
        enabled: true,
        auto_download: true,
        release_filter: {
          albums: true,
          singles: true,
          eps: true,
          live: false,
          compilations: false,
          remixes: false,
        },
        download_priority: 'normal',
      },
    ],
  }

  const samplePreview: ImportPreview = {
    total: 1,
    new: 1,
    existing: 0,
    would_update: 0,
    unchanged: 0,
    invalid: 0,
    duplicates: 0,
    items: [
      {
        index: 0,
        artist_name: 'Daft Punk',
        provider: 'deezer',
        artist_source_id: '27',
        enabled: true,
        auto_download: true,
        release_filter: {
          albums: true,
          singles: true,
          eps: true,
          live: false,
          compilations: false,
          remixes: false,
        },
        download_priority: 'normal',
        status: 'new',
      },
    ],
  }

  const sampleResult: ImportResult = {
    created: 1,
    updated: 0,
    unchanged: 0,
    failed: 0,
  }

  it('renders the initial pick step', () => {
    render(
      <SubscriptionImportDialog
        open={true}
        onOpenChange={() => {}}
        onImportSuccess={() => {}}
      />,
    )

    expect(screen.getByText('Abonnements importieren')).toBeDefined()
    expect(
      screen.getByText('Klicke zum Auswählen oder ziehe die JSON-Datei hierher'),
    ).toBeDefined()
  })

  it('runs two-stage import: preview then apply', async () => {
    let previewCalled = false
    let applyCalled = false
    let successCalled = false

    globalThis.fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = typeof input === 'string' ? input : input.toString()
      const method = init?.method ?? 'GET'

      if (url.includes('/subscriptions/import/preview') && method === 'POST') {
        previewCalled = true
        return new Response(JSON.stringify({ data: samplePreview }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        })
      }

      if (url.includes('/subscriptions/import/apply') && method === 'POST') {
        applyCalled = true
        return new Response(JSON.stringify({ data: sampleResult }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        })
      }

      throw new Error(`Unexpected fetch call: ${method} ${url}`)
    }) as typeof fetch

    render(
      <SubscriptionImportDialog
        open={true}
        onOpenChange={() => {}}
        onImportSuccess={() => {
          successCalled = true
        }}
      />,
    )

    const file = new File([JSON.stringify(sampleExport)], 'subs.json', {
      type: 'application/json',
    })
    const input = document.body.querySelector('input[type="file"]') as HTMLInputElement
    expect(input).not.toBeNull()

    fireEvent.change(input, { target: { files: [file] } })

    // Step 2: Preview should appear
    expect(await screen.findByText('1 Abonnements importieren')).toBeDefined()
    expect(previewCalled).toBe(true)
    expect(screen.getByText('Vorschau der Einträge (1)')).toBeDefined()

    // Click apply button
    fireEvent.click(screen.getByRole('button', { name: /1 Abonnements importieren/ }))

    // Step 3: Result summary
    expect(await screen.findByText('Import erfolgreich angewendet')).toBeDefined()
    expect(applyCalled).toBe(true)
    expect(successCalled).toBe(true)
    expect(screen.getByText('Neu erstellt')).toBeDefined()
  })

  it('rejects invalid JSON files gracefully', async () => {
    render(
      <SubscriptionImportDialog
        open={true}
        onOpenChange={() => {}}
        onImportSuccess={() => {}}
      />,
    )

    const invalidFile = new File(['not json at all'], 'broken.json', {
      type: 'application/json',
    })
    const input = document.body.querySelector('input[type="file"]') as HTMLInputElement
    expect(input).not.toBeNull()
    fireEvent.change(input, { target: { files: [invalidFile] } })

    expect(await screen.findByText('Die Datei enthält kein gültiges JSON.')).toBeDefined()
  })
})
