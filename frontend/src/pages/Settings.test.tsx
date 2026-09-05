import { afterEach, beforeEach, describe, expect, it, mock } from 'bun:test'
import { fireEvent, render, screen } from '@testing-library/react'

import { Settings } from './Settings'

// Mock APIs used by Settings
mock.module('@/lib/api/settings', () => ({
  getHealth: async () => ({
    version: '0.17.3',
    schema_version: 9,
    status: 'ok',
    checks: {
      application: 'ok',
      database: 'ok',
      ffmpeg: 'ok',
      ffprobe: 'ok',
      'yt-dlp': 'ok',
    },
  }),
  getSettings: async () => ({
    concurrent_downloads: 2,
    rate_limit_kbs: 0,
    download_window_start: '',
    download_window_end: '',
    skip_existing: true,
    default_metadata_provider: 'ytmusic',
    preferred_audio_format: 'opus',
    default_release_filter: ['album', 'single', 'ep'],
    library_path: '/music',
    match_min_score: 80,
    match_duration_tolerance_ms: 5000,
    allow_transcode: true,
  }),
  listProviders: async () => [
    {
      id: 'ytmusic',
      name: 'YouTube Music',
      kind: 'media',
      enabled: true,
      authenticated: false,
    },
  ],
  updateSettings: async () => {},
}))

mock.module('@/lib/api/storage', () => ({
  getStorageStatus: async () => ({
    library: {
      path: '/music',
      guard_configured: true,
      guard_status: 'verified',
      status: 'healthy',
      fs_type: 'NFS',
      total_bytes: 1000000,
      free_bytes: 500000,
      used_bytes: 500000,
      free_percent: 50,
      min_free_bytes: 1000,
      is_network_fs: true,
      last_checked_at: new Date().toISOString(),
    },
    staging: {
      path: '/data/staging',
      total_bytes: 1000000,
      free_bytes: 500000,
      used_bytes: 500000,
      min_free_bytes: 1000,
      max_bytes: 10000,
      current_staged_bytes: 500,
      active_items: 0,
      active_partials: 0,
    },
    queue: {
      paused: false,
      waiting_storage_items: 0,
      waiting_space_items: 0,
      retry_wait_items: 0,
    },
  }),
}))

mock.module('@/lib/api/system', () => ({
  getUpdateStatus: async () => ({
    current_version: '0.17.3',
    latest_version: '0.17.3',
    state: 'up_to_date',
    checked_at: new Date().toISOString(),
    cached: true,
  }),
}))

describe('Settings Page Local Tabs', () => {
  const originalLocation = window.location

  beforeEach(() => {
    window.location.hash = ''
  })

  afterEach(() => {
    window.location.hash = ''
  })

  it('renders Servereinstellungen header and 4 local tabs', async () => {
    render(<Settings />)

    expect(screen.getByRole('heading', { level: 1, name: 'Servereinstellungen' })).toBeDefined()
    expect(screen.getByText('Konfiguration, Diagnose und Dienste verwalten.')).toBeDefined()

    // 4 Local Tabs
    expect(screen.getByRole('button', { name: /Allgemein/i })).toBeDefined()
    expect(screen.getByRole('button', { name: /Downloads/i })).toBeDefined()
    expect(screen.getByRole('button', { name: /Speicher/i })).toBeDefined()
    expect(screen.getByRole('button', { name: /Provider/i })).toBeDefined()
  })

  it('defaults to Allgemein tab showing Systemdiagnose', async () => {
    render(<Settings />)

    expect(screen.getByText('Systemdiagnose')).toBeDefined()
    expect(screen.queryByText('Download-Verhalten & Automation')).toBeNull()
    expect(screen.queryByText('Speicher & Netzwerk-Storage')).toBeNull()
  })

  it('switches to Downloads tab when clicked', async () => {
    render(<Settings />)

    const downloadsTab = screen.getByRole('button', { name: /Downloads/i })
    fireEvent.click(downloadsTab)

    expect(window.location.hash).toBe('#downloads')
    expect(await screen.findByText('Download-Verhalten & Automation')).toBeDefined()
    expect(screen.queryByText('Systemdiagnose')).toBeNull()
  })

  it('switches to Speicher tab when clicked', async () => {
    render(<Settings />)

    const storageTab = screen.getByRole('button', { name: /Speicher/i })
    fireEvent.click(storageTab)

    expect(window.location.hash).toBe('#storage')
    expect(await screen.findByText('Speicher & Netzwerk-Storage')).toBeDefined()
    expect(screen.queryByText('Systemdiagnose')).toBeNull()
  })

  it('switches to Provider tab when clicked', async () => {
    render(<Settings />)

    const providersTab = screen.getByRole('button', { name: /Provider/i })
    fireEvent.click(providersTab)

    expect(window.location.hash).toBe('#providers')
    expect(await screen.findByText('Woher Metadaten kommen und woher die Audioquellen.')).toBeDefined()
    expect(screen.getByText('YouTube Music')).toBeDefined()
    expect(screen.queryByText('Download-Verhalten & Automation')).toBeNull()
  })

  it('respects initial hashes: #health, #updates, #startup, #downloads, #storage, #providers', async () => {
    // #updates -> general tab
    window.location.hash = '#updates'
    const { unmount: u1 } = render(<Settings />)
    expect(screen.getByText('System & Updates')).toBeDefined()
    u1()

    // #startup -> general tab
    window.location.hash = '#startup'
    const { unmount: u2 } = render(<Settings />)
    expect(screen.getByText('Systemdiagnose')).toBeDefined()
    u2()

    // #health -> general tab
    window.location.hash = '#health'
    const { unmount: u3 } = render(<Settings />)
    expect(screen.getByText('Systemdiagnose')).toBeDefined()
    u3()

    // #downloads -> downloads tab
    window.location.hash = '#downloads'
    const { unmount: u4 } = render(<Settings />)
    expect(await screen.findByText('Download-Verhalten & Automation')).toBeDefined()
    u4()

    // #storage -> storage tab
    window.location.hash = '#storage'
    const { unmount: u5 } = render(<Settings />)
    expect(await screen.findByText('Speicher & Netzwerk-Storage')).toBeDefined()
    u5()

    // #providers -> providers tab
    window.location.hash = '#providers'
    const { unmount: u6 } = render(<Settings />)
    expect(await screen.findByText('Woher Metadaten kommen und woher die Audioquellen.')).toBeDefined()
    u6()
  })

  it('supports browser back/forward popstate navigation between tabs', async () => {
    window.location.hash = '#general'
    render(<Settings />)
    expect(screen.getByText('Systemdiagnose')).toBeDefined()

    // Simulate browser back/forward to #downloads
    window.location.hash = '#downloads'
    fireEvent(window, new Event('popstate'))
    expect(await screen.findByText('Download-Verhalten & Automation')).toBeDefined()
    expect(screen.queryByText('Systemdiagnose')).toBeNull()

    // Simulate browser back to #general
    window.location.hash = '#health'
    fireEvent(window, new Event('popstate'))
    expect(await screen.findByText('Systemdiagnose')).toBeDefined()
    expect(screen.queryByText('Download-Verhalten & Automation')).toBeNull()
  })
})
