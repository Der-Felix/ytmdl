import { DownloadButton } from '@/components/downloads/DownloadButton'
import { Cover } from '@/components/music/Cover'
import { downloadRelease } from '@/lib/api/jobs'
import { Link, paths } from '@/lib/router'
import { formatYear, pluralize } from '@/lib/utils/format'
import type { LibraryRelease, Release } from '@/types/api'

interface ReleaseCardProps {
  release: Release | LibraryRelease
  /** Which provider the id belongs to; falls back to the release's own. */
  provider?: string
  href?: string
  isLocal?: boolean
}

/**
 * One release in a grid.
 */
function ReleaseCard({ release, provider, href, isLocal }: ReleaseCardProps) {
  const source = provider ?? release.provider
  const year = formatYear(release.year)
  const targetHref = href ?? (isLocal ? paths.libraryRelease(release.id) : paths.release(release.source_id || release.id, source))
  const isLibRelease = 'track_count_in_library' in release

  return (
    <div className="panel panel-interactive group relative flex h-full flex-col gap-3 p-3">
      <Cover src={release.cover_url} alt="" className="w-full" />

      <div className="min-w-0 flex-1 space-y-1 px-1">
        <Link
          href={targetHref}
          className="focus-ring block rounded-md after:absolute after:inset-0 after:content-['']"
        >
          <span className="line-clamp-2 font-heading text-sm leading-snug font-medium text-foreground">
            {release.title}
          </span>
        </Link>

        <p className="truncate text-xs text-muted-foreground">
          {isLibRelease ? (
            `${year ? `${year} · ` : ''}${pluralize(release.track_count_in_library, 'Track')}`
          ) : (
            [year, release.track_count > 0 ? pluralize(release.track_count, 'Track') : '']
              .filter(Boolean)
              .join(' · ') || 'Kein Erscheinungsjahr'
          )}
        </p>
      </div>

      {!isLocal && (
        <div className="relative z-10 mt-auto flex justify-end px-1 pb-1">
          <DownloadButton
            size="sm"
            iconOnly
            label={`„${release.title}" herunterladen`}
            start={(signal) =>
              downloadRelease(
                { release_id: release.source_id || release.id, provider: source },
                signal,
              )
            }
          />
        </div>
      )}
    </div>
  )
}

export { ReleaseCard }
