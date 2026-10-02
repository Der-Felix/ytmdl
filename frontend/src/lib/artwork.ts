/** Same-origin artwork from catalog-owned local files. Never proxies an arbitrary URL. */
export function libraryArtwork(kind: 'artists' | 'releases' | 'tracks', id: string): string {
  return `/api/v1/library/${kind}/${encodeURIComponent(id)}/artwork`
}
