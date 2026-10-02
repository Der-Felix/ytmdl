import { Heart } from 'lucide-react'
import { Cover } from '@/components/music/Cover'
import { libraryArtwork } from '@/lib/artwork'
import type { LibraryTrack } from '@/types/api'

export function FavoriteArtwork({ tracks }: { tracks: Pick<LibraryTrack, 'id' | 'release_id' | 'cover_url' | 'title'>[] }) {
  const seen = new Set<string>()
  const preview = tracks.filter((track) => {
    const key = track.release_id || track.cover_url || track.id
    if (seen.has(key)) return false
    seen.add(key)
    return true
  }).slice(0, 4)

  return (
    <div className="size-32 sm:size-40 overflow-hidden rounded-2xl bg-gradient-to-br from-rose-950/60 to-neutral-900 border border-rose-500/20 flex items-center justify-center shrink-0 shadow-2xl">
      {preview.length > 0 ? (
        <div className={`grid size-full ${preview.length > 1 ? 'grid-cols-2' : 'grid-cols-1'}`} aria-label="Cover deiner Lieblingstitel">
          {preview.map((track, index) => (
            <Cover
              key={track.id}
              src={libraryArtwork('tracks', track.id)}
              fallbackSrc={track.cover_url}
              alt={track.title}
              className={`size-full rounded-none border-0 ${preview.length === 3 && index === 2 ? 'col-span-2 aspect-auto' : ''}`}
            />
          ))}
        </div>
      ) : <Heart className="size-16 sm:size-20 text-rose-500 fill-rose-500/30" />}
    </div>
  )
}
