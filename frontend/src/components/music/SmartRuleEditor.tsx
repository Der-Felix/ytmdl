import { useState } from 'react'
import { Input } from '@/components/ui/input'
import { useAsync } from '@/hooks/useAsync'
import { libraryArtists, libraryGenres } from '@/lib/api/library'
import type { SmartRules } from '@/types/playlist'
export const defaultSmartRules: SmartRules = { sort: 'recent', limit: 100 }
export function SmartRuleEditor({
  value,
  onChange,
}: {
  value: SmartRules | null
  onChange: (v: SmartRules | null) => void
}) {
  const [query, setQuery] = useState('')
  const genres = useAsync((signal) => libraryGenres(signal), [])
  const artists = useAsync((signal) => libraryArtists({ q: query, limit: 50, signal }), [query])
  const update = (patch: Partial<SmartRules>) =>
    onChange({ ...defaultSmartRules, ...value, ...patch })
  return (
    <fieldset className="space-y-3 rounded-xl border border-white/10 p-3 text-sm">
      <label className="flex items-center gap-2">
        <input
          type="checkbox"
          checked={value !== null}
          onChange={(e) => onChange(e.target.checked ? { ...defaultSmartRules } : null)}
        />
        Intelligente Playlist
      </label>
      {value && (
        <div className="space-y-3">
          <p className="text-xs text-muted-foreground">
            Alle gewählten Bedingungen müssen passen. Die Titel aktualisieren sich bei jedem Öffnen.
          </p>
          <label className="block">
            Genre
            <select
              aria-label="Playlist-Genre"
              className="mt-1 w-full rounded-md border border-border bg-background p-2"
              value={value.genre || ''}
              onChange={(e) => update({ genre: e.target.value })}
            >
              <option value="">Alle Genres</option>
              {genres.state.status === 'success' &&
                genres.state.data.map((g) => <option key={g}>{g}</option>)}
            </select>
          </label>
          <label className="block">
            Künstler suchen
            <Input
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="Name eingeben"
            />
          </label>
          <select
            aria-label="Playlist-Künstler"
            className="w-full rounded-md border border-border bg-background p-2"
            value={value.artist_id || ''}
            onChange={(e) => update({ artist_id: e.target.value })}
          >
            <option value="">Alle Künstler</option>
            {value.artist_id &&
              artists.state.status === 'success' &&
              !artists.state.data.items.some((a) => a.id === value.artist_id) && (
                <option value={value.artist_id}>Gewählter Künstler</option>
              )}
            {artists.state.status === 'success' &&
              artists.state.data.items.map((a) => (
                <option key={a.id} value={a.id}>
                  {a.name}
                </option>
              ))}
          </select>
          <label className="flex items-center gap-2">
            <input
              type="checkbox"
              checked={!!value.favorites}
              onChange={(e) => update({ favorites: e.target.checked })}
            />
            Nur meine Favoriten
          </label>
          <label className="block">
            Hinzugefügt
            <select
              aria-label="Playlist-Zeitraum"
              className="mt-1 w-full rounded-md border border-border bg-background p-2"
              value={value.added_days || 0}
              onChange={(e) => update({ added_days: Number(e.target.value) })}
            >
              <option value={0}>Jederzeit</option>
              {[7, 30, 90, 365].map((d) => (
                <option key={d} value={d}>
                  In den letzten {d} Tagen
                </option>
              ))}
            </select>
          </label>
          <label className="block">
            Sortierung
            <select
              aria-label="Playlist-Sortierung"
              className="mt-1 w-full rounded-md border border-border bg-background p-2"
              value={value.sort}
              onChange={(e) => update({ sort: e.target.value as SmartRules['sort'] })}
            >
              <option value="recent">Zuletzt hinzugefügt</option>
              <option value="title">Titel A–Z</option>
              <option value="frequent">Am häufigsten gehört</option>
              <option value="last_played">Zuletzt gehört</option>
            </select>
          </label>
          <label className="block">
            Maximale Titelzahl
            <Input
              type="number"
              min={1}
              max={500}
              value={value.limit}
              onChange={(e) => update({ limit: Number(e.target.value) })}
            />
          </label>
          {(genres.state.status === 'error' || artists.state.status === 'error') && (
            <p role="alert">Filter konnten nicht vollständig geladen werden.</p>
          )}
        </div>
      )}
    </fieldset>
  )
}
