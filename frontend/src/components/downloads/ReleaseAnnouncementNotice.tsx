import { CalendarDaysIcon } from 'lucide-react'
import type { ReleaseAnnouncement } from '@/types/api'

function calendarDate(value: string): Date | null {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(value)) return null
  const date = new Date(`${value}T00:00:00Z`)
  return Number.isFinite(date.getTime()) && date.toISOString().slice(0, 10) === value ? date : null
}

/** A checked announcement is context, never a source-availability guarantee. */
export function ReleaseAnnouncementNotice({ announcement, now = new Date() }: {
  announcement?: ReleaseAnnouncement
  now?: Date
}) {
  if (!announcement) return null
  const date = calendarDate(announcement.date)
  const checked = new Date(announcement.checked_at)
  if (!date || !Number.isFinite(checked.getTime()) || !announcement.source.trim()) return null
  const upcoming = announcement.date > now.toISOString().slice(0, 10)
  const format = (value: Date) => value.toLocaleDateString('de-DE', {
    day: 'numeric', month: 'long', year: 'numeric', timeZone: 'UTC',
  })
  return (
    <aside className="flex items-start gap-2.5 rounded-lg border border-border bg-muted/20 p-3 text-xs">
      <CalendarDaysIcon aria-hidden className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
      <div className="space-y-1">
        <p className="font-medium text-foreground">{upcoming ? 'Veröffentlichung angekündigt' : 'Angekündigter Termin erreicht'}: {format(date)}</p>
        <p className="text-muted-foreground">Quelle: {announcement.source.slice(0, 120)} · Geprüft am {format(checked)}.</p>
        <p className="text-muted-foreground">{upcoming
          ? 'Einzelne Titel können schon vorher verfügbar sein. Der Termin garantiert keine verfügbare Downloadquelle.'
          : 'Ob die fehlenden Aufnahmen inzwischen verfügbar sind, muss erneut geprüft werden.'} Es ist kein automatischer Neustart für diesen Termin eingerichtet.</p>
      </div>
    </aside>
  )
}
