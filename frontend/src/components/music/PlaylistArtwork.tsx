import { ListMusic } from 'lucide-react'
import { Cover } from '@/components/music/Cover'
import { libraryArtwork } from '@/lib/artwork'
import { cn } from '@/lib/utils'
import type { LibraryTrack } from '@/types/api'
import { playlistPreview } from '@/lib/playlist-artwork'
type ArtworkTrack = Pick<LibraryTrack, 'id' | 'release_id' | 'cover_url' | 'title'>

export function PlaylistArtwork({ tracks, name, className }: { tracks: ArtworkTrack[]; name: string; className?: string }) {
  const preview = playlistPreview(tracks)
  return <div aria-label={`Cover der Playlist ${name}`} className={cn('aspect-square overflow-hidden rounded-2xl border border-white/10 bg-gradient-to-br from-primary/15 to-neutral-900', className)}>
    {preview.length ? <div className={`grid size-full ${preview.length > 1 ? 'grid-cols-2 grid-rows-2' : ''}`}>
      {Array.from({ length: preview.length > 1 ? 4 : 1 }, (_, index) => {
        const track = preview[index % preview.length]!
        return <Cover key={index} src={libraryArtwork('tracks', track.id)} alt={track.title} className="size-full aspect-auto rounded-none border-0" />
      })}
    </div> : <div className="flex size-full items-center justify-center"><ListMusic className="size-1/3 text-primary/60" /></div>}
  </div>
}
