import { afterEach, describe, expect, it } from 'bun:test'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { LibraryArtist } from './LibraryArtist'
import { PlayerProvider } from '@/contexts/PlayerContext'

const originalFetch = globalThis.fetch
afterEach(() => { globalThis.fetch = originalFetch })

describe('artist genres editor', () => {
  it('preserves the draft after failure and allows an explicit clear', async () => {
    let fail = true
    const writes: unknown[] = []
    globalThis.fetch = (async (_input, init) => {
      if (init?.method === 'PUT') {
        const body = JSON.parse(init.body as string)
        writes.push(body)
        return new Response(JSON.stringify(fail ? { error: { code: 'INVALID_REQUEST', message: 'Zu viele Genres.' } } : { data: [] }), { status: fail ? 400 : 200 })
      }
      const url = String(_input)
      const data = url.includes('/library/artists/') ? { artist: { id: 'art-genre', name: 'Genre Artist', provider: 'ytmusic', source_id: 'fixture', source_url: '', genres: ['Pop'] }, releases: [], tracks: [], release_count: 0, track_count: 0, total_size_bytes: 0 } : []
      return new Response(JSON.stringify({ data }))
    }) as typeof fetch
    render(<PlayerProvider><LibraryArtist id="art-genre" /></PlayerProvider>)
    fireEvent.click(await screen.findByRole('button', { name: 'Genres bearbeiten' }))
    const input = screen.getByRole('textbox', { name: 'Genres, durch Kommas getrennt' })
    fireEvent.change(input, { target: { value: 'Hip-Hop' } })
    fireEvent.submit(input.closest('form')!)
    await screen.findByText('Zu viele Genres.')
    expect((input as HTMLInputElement).value).toBe('Hip-Hop')
    fail = false
    fireEvent.change(input, { target: { value: '' } })
    fireEvent.submit(input.closest('form')!)
    await screen.findByText('Genres gespeichert.')
    await waitFor(() => expect(screen.getByText('Ohne Genre')).toBeTruthy())
    expect(writes).toEqual([{ genres: ['Hip-Hop'] }, { genres: [''] }])
  })
})
