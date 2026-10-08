import { afterEach, expect, test } from 'bun:test'
import { cleanup, render, screen } from '@testing-library/react'
import { ReleaseAnnouncementNotice } from './ReleaseAnnouncementNotice'

afterEach(cleanup)
const announcement = { date: '2026-10-23', source: 'Labelkatalog', checked_at: '2026-10-07T12:00:00Z' }
test('announcement provides context without promising availability or scheduling', () => {
 render(<ReleaseAnnouncementNotice announcement={announcement} now={new Date('2026-10-07T12:00:00Z')} />)
 expect(screen.getByText(/Veröffentlichung angekündigt: 23. Oktober 2026/)).toBeTruthy()
 expect(screen.getByText(/garantiert keine verfügbare Downloadquelle/)).toBeTruthy()
 expect(screen.getByText(/kein automatischer Neustart/)).toBeTruthy()
})
test('reached date requests a fresh check rather than claiming an unreleased album', () => {
 render(<ReleaseAnnouncementNotice announcement={announcement} now={new Date('2026-10-23T00:01:00Z')} />)
 expect(screen.getByText(/Angekündigter Termin erreicht/)).toBeTruthy()
 expect(screen.getByText(/muss erneut geprüft werden/)).toBeTruthy()
 expect(screen.queryByText(/Veröffentlichung angekündigt/)).toBeNull()
})
test('invalid and impossible dates are not rendered', () => {
 for (const date of ['2026-02-31', 'unknown', '2026-10-23T00:00:00Z']) {
  const { container, unmount } = render(<ReleaseAnnouncementNotice announcement={{ ...announcement, date }} />)
  expect(container.textContent).toBe('')
  unmount()
 }
})
