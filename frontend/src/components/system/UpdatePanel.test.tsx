import { afterEach, beforeEach, describe, expect, it } from 'bun:test'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'

import { UpdatePanel } from './UpdatePanel'
import type { UpdateStatus } from '@/types/api'

const upToDateStatus: UpdateStatus = {
  current_version: '0.14.1',
  latest_version: '0.14.1',
  state: 'up_to_date',
  checked_at: new Date().toISOString(),
  cached: true,
}

const updateAvailableStatus: UpdateStatus = {
  current_version: '0.14.1',
  latest_version: '0.15.0',
  state: 'update_available',
  release_name: 'YTMDL v0.15.0 Release',
  published_at: new Date().toISOString(),
  release_url: 'https://github.com/Der-Felix/ytmdl/releases/tag/v0.15.0',
  release_notes: 'Exciting new features in v0.15.0',
  checked_at: new Date().toISOString(),
  cached: false,
}

const noPublicReleaseStatus: UpdateStatus = {
  current_version: '0.14.1',
  state: 'no_public_release',
  checked_at: new Date().toISOString(),
  cached: true,
}

const unavailableStatus: UpdateStatus = {
  current_version: '0.14.1',
  state: 'unavailable',
  checked_at: new Date().toISOString(),
  cached: false,
}

const disabledStatus: UpdateStatus = {
  current_version: '0.14.1',
  state: 'disabled',
  checked_at: new Date().toISOString(),
  cached: false,
}

let originalFetch: typeof fetch

beforeEach(() => {
  originalFetch = globalThis.fetch
})

afterEach(() => {
  globalThis.fetch = originalFetch
})

describe('UpdatePanel', () => {
  it('renders up_to_date state correctly', () => {
    render(<UpdatePanel initialData={upToDateStatus} />)

    expect(screen.getByText('YTMDL Version')).toBeDefined()
    expect(screen.getByText('Aktuell')).toBeDefined()
    expect(screen.getAllByText('0.14.1').length).toBeGreaterThanOrEqual(1)
    expect(screen.getByText('Nach Updates suchen')).toBeDefined()
  })

  it('renders update_available state with release details, docs link, and host command', () => {
    render(<UpdatePanel initialData={updateAvailableStatus} />)

    expect(screen.getByText('Update verfügbar')).toBeDefined()
    expect(screen.getByText('0.15.0')).toBeDefined()
    expect(screen.getByText('YTMDL v0.15.0 Release')).toBeDefined()
    expect(screen.getByText('Exciting new features in v0.15.0')).toBeDefined()
    expect(screen.getByText('Auf dem YTMDL-Host ausführen:')).toBeDefined()
    expect(screen.getByText('ytmdlctl update')).toBeDefined()

    const githubLink = screen.getByRole('link', { name: /Auf GitHub ansehen/i })
    expect(githubLink).toBeDefined()
    expect(githubLink.getAttribute('href')).toBe('https://github.com/Der-Felix/ytmdl/releases/tag/v0.15.0')
    expect(githubLink.getAttribute('target')).toBe('_blank')
    expect(githubLink.getAttribute('rel')).toBe('noopener noreferrer')

    const docsLink = screen.getByRole('link', { name: /Dokumentation/i })
    expect(docsLink).toBeDefined()
    expect(docsLink.getAttribute('href')).toBe('/ytmdl/updates')
    expect(docsLink.getAttribute('target')).toBe('_blank')
  })

  it('copies host update command to clipboard on button click', async () => {
    let copiedText = ''
    Object.defineProperty(navigator, 'clipboard', {
      value: {
        writeText: async (text: string) => {
          copiedText = text
        },
      },
      configurable: true,
    })

    render(<UpdatePanel initialData={updateAvailableStatus} />)

    const copyBtn = screen.getByRole('button', { name: /Kopieren/i })
    expect(copyBtn).toBeDefined()

    fireEvent.click(copyBtn)

    await waitFor(() => {
      expect(copiedText).toBe('ytmdlctl update')
      expect(screen.getByText('Kopiert!')).toBeDefined()
    })
  })

  it('renders release notes safely as plain text without HTML execution', () => {
    const xssStatus: UpdateStatus = {
      ...updateAvailableStatus,
      release_notes: '<img src=x onerror=alert(1)><b class="injected-bold">Plain Bold</b>',
    }

    render(<UpdatePanel initialData={xssStatus} />)

    const pre = screen.getByText(/<img src=x/i)
    expect(pre.tagName.toLowerCase()).toBe('pre')
    expect(pre.textContent).toContain('<img src=x onerror=alert(1)><b class="injected-bold">Plain Bold</b>')
    expect(pre.querySelector('img')).toBeNull()
    expect(pre.querySelector('.injected-bold')).toBeNull()
  })

  it('renders no_public_release state with helpful message', () => {
    render(<UpdatePanel initialData={noPublicReleaseStatus} />)

    expect(screen.getByText('Kein Public Release')).toBeDefined()
    expect(screen.getByText(/Noch keine öffentliche Stable-Version auf GitHub verfügbar/i)).toBeDefined()
  })

  it('renders unavailable state with message', () => {
    render(<UpdatePanel initialData={unavailableStatus} />)

    expect(screen.getByText('Nicht verfügbar')).toBeDefined()
    expect(screen.getByText(/Updateprüfung momentan nicht verfügbar/i)).toBeDefined()
  })

  it('renders disabled state with button disabled', () => {
    render(<UpdatePanel initialData={disabledStatus} />)

    expect(screen.getByText('Deaktiviert')).toBeDefined()
    const button = screen.getByRole('button', { name: /Nach Updates suchen/i })
    expect(button.getAttribute('disabled')).toBeDefined()
    expect(screen.getByText(/MUSICDL_UPDATE_CHECKS_ENABLED=false/i)).toBeDefined()
  })

  it('triggers manual refresh check on button click', async () => {
    const refreshedData: UpdateStatus = {
      ...updateAvailableStatus,
      cached: false,
    }

    globalThis.fetch = async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.includes('/api/v1/system/update/check')) {
        return new Response(JSON.stringify({ data: refreshedData }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        })
      }
      return new Response('{}', { status: 200 })
    }

    render(<UpdatePanel initialData={upToDateStatus} />)

    const button = screen.getByRole('button', { name: /Nach Updates suchen/i })
    fireEvent.click(button)

    await waitFor(() => {
      expect(screen.getByText('Update verfügbar')).toBeDefined()
      expect(screen.getByText('YTMDL v0.15.0 Release')).toBeDefined()
    })
  })
})
