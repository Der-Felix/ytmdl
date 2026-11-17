import { describe, expect, it } from 'bun:test'
import { act, render, screen } from '@testing-library/react'

import { Toaster } from '@/components/ui/toast'
import { notifyError, notifySuccess } from '@/lib/notify'

describe('notify', () => {
  it('shows a result as a toast that names what happened', async () => {
    render(<Toaster />)
    act(() => {
      notifySuccess('Zur Warteschlange hinzugefügt', 'Titel 1')
    })
    expect(await screen.findByText('Zur Warteschlange hinzugefügt')).toBeTruthy()
    expect(screen.getByText('Titel 1')).toBeTruthy()
    expect(screen.getByRole('region', { name: 'Meldungen' })).toBeTruthy()
  })

  it('announces a failure at once and carries the error message', async () => {
    render(<Toaster />)
    act(() => {
      notifyError('Reihenfolge konnte nicht gespeichert werden', new Error('Reihenfolge hat sich geändert.'))
    })
    const alert = await screen.findByRole('alert')
    expect(alert.textContent).toContain('Reihenfolge konnte nicht gespeichert werden')
    expect(alert.textContent).toContain('Reihenfolge hat sich geändert.')
  })
})
