import { beforeEach, describe, expect, it, mock } from 'bun:test'
import { fireEvent, render, screen, within } from '@testing-library/react'

import { AreaSwitch } from './AreaSwitch'

beforeEach(() => {
  window.history.replaceState(null, '', '/profile')
})

describe('AreaSwitch', () => {
  it('marks the current area and links each segment to its start page', () => {
    render(<AreaSwitch area="listen" />)
    const nav = screen.getByRole('navigation', { name: 'Bereich' })
    const listen = within(nav).getByRole('link', { name: 'Hören' })
    const download = within(nav).getByRole('link', { name: 'Laden' })

    expect(listen.getAttribute('aria-current')).toBe('true')
    expect(download.getAttribute('aria-current')).toBeNull()
    expect(listen.getAttribute('href')).toBe('/listen')
    expect(download.getAttribute('href')).toBe('/search')
  })

  it('navigates to the start page of the chosen area', () => {
    const onNavigate = mock(() => {})
    render(<AreaSwitch area="listen" onNavigate={onNavigate} />)
    fireEvent.click(screen.getByRole('link', { name: 'Laden' }))
    expect(window.location.pathname).toBe('/search')
    expect(onNavigate).toHaveBeenCalledTimes(1)
  })

  it('is reached with Tab in reading order, with the shared focus ring', () => {
    render(
      <>
        <button type="button">davor</button>
        <AreaSwitch area="download" />
      </>,
    )
    const links = within(screen.getByRole('navigation', { name: 'Bereich' })).getAllByRole('link')
    expect(links.map((link) => link.textContent)).toEqual(['Hören', 'Laden'])
    for (const link of links) {
      // Real anchors with an href: focusable, and Enter follows them.
      expect(link.tagName).toBe('A')
      expect(link.getAttribute('tabindex')).toBeNull()
      expect(link.className).toContain('focus-ring')
      link.focus()
      expect(document.activeElement).toBe(link)
    }
    expect(links[1]!.getAttribute('aria-current')).toBe('true')
  })

  it('tells a listener how many downloads are open', () => {
    render(<AreaSwitch area="listen" activeDownloads={1643} />)
    const download = screen.getByRole('link', { name: /Laden/ })
    expect(download.textContent).toContain('1.643')
    expect(download.textContent).toContain('1.643 offene Downloads')
  })

  it('shows no count while the user is in Laden or nothing is open', () => {
    const { unmount } = render(<AreaSwitch area="download" activeDownloads={12} />)
    expect(screen.getByRole('link', { name: 'Laden' }).textContent).toBe('Laden')
    unmount()
    render(<AreaSwitch area="listen" activeDownloads={0} />)
    expect(screen.getByRole('link', { name: 'Laden' }).textContent).toBe('Laden')
  })
})
