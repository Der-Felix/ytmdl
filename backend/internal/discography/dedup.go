// Package discography resolves the complete catalogue of an artist and reduces
// it to the set of distinct recordings that should end up in the library.
package discography

import (
	"sort"
	"strings"

	"ytdm/backend/internal/matcher"
	"ytdm/backend/internal/music"
	"ytdm/backend/internal/storage"
)

// DefaultDurationToleranceMS is the maximum difference in runtime at which two
// otherwise identical recordings are still considered the same.
const DefaultDurationToleranceMS = 4000

// DedupOptions configures the deduplication pass.
type DedupOptions struct {
	// DurationToleranceMS is the runtime window within which two tracks with
	// identical artist, title and version count as the same recording.
	DurationToleranceMS int
}

// MergeReason explains why a set of tracks was collapsed.
type MergeReason string

const (
	// ReasonUnique marks a group that never absorbed another track.
	ReasonUnique MergeReason = "unique"
	// ReasonTrackID marks a group merged by stable track or source identifier.
	ReasonTrackID MergeReason = "track_id"
	// ReasonISRC marks a group that was merged because of a shared ISRC.
	ReasonISRC MergeReason = "isrc"
	// ReasonMetadata marks a group merged by artist, title, version and
	// runtime.
	ReasonMetadata MergeReason = "metadata"
)

// Group is one distinct recording plus the duplicates that were folded into it.
type Group struct {
	// Track is the representative that will be downloaded.
	Track music.Track
	// Duplicates are the other occurrences of the same recording.
	Duplicates []music.Track
	// Key is the identity key the group was built from.
	Key string
	// Reason records how the group came about.
	Reason MergeReason
}

// Deduplicate collapses tracks that describe the same recording. Identity is
// decided by ISRC first and by normalised artist, title, version markers and
// runtime second. Genuine variants — live, instrumental, remix, acoustic — keep
// their own group because their version markers differ.
func Deduplicate(tracks []music.Track, opts DedupOptions) []Group {
	tolerance := opts.DurationToleranceMS
	if tolerance <= 0 {
		tolerance = DefaultDurationToleranceMS
	}
	if len(tracks) == 0 {
		return nil
	}

	uf := newUnionFind(len(tracks))
	keys := make([]string, len(tracks))

	// Pass one: stable Catalog Track ID always merges, as it represents the exact
	// same underlying catalog/provider track item across releases.
	firstByID := make(map[string]int)
	for i, t := range tracks {
		id := strings.TrimSpace(t.ID)
		if id == "" {
			continue
		}
		if prev, ok := firstByID[id]; ok {
			uf.union(prev, i)
			continue
		}
		firstByID[id] = i
	}

	// Pass two: stable SourceProvider + SourceID always merges.
	firstBySource := make(map[string]int)
	for i, t := range tracks {
		if t.SourceProvider == "" || t.SourceID == "" {
			continue
		}
		key := t.SourceProvider + ":" + t.SourceID
		if prev, ok := firstBySource[key]; ok {
			uf.union(prev, i)
			continue
		}
		firstBySource[key] = i
	}

	// Pass three: identical base title and version markers, a matching runtime
	// and at least one artist in common. Requiring an overlapping credit keeps
	// different artists apart while tolerating the order and featuring
	// inconsistencies that providers introduce between releases.
	type cluster struct {
		rep        int
		durationMS int
		artists    map[string]struct{}
	}
	buckets := make(map[string][]cluster)
	artistSets := make([]map[string]struct{}, len(tracks))
	for i, t := range tracks {
		keys[i] = IdentityKey(t)
		artistSets[i] = ArtistKeySet(t)

		bucket := titleBucket(t)
		found := false
		for _, c := range buckets[bucket] {
			if durationsMatch(t.DurationMS, c.durationMS, tolerance) && intersects(artistSets[i], c.artists) {
				uf.union(i, c.rep)
				found = true
				break
			}
		}
		if !found {
			buckets[bucket] = append(buckets[bucket], cluster{
				rep: i, durationMS: t.DurationMS, artists: artistSets[i],
			})
		}
	}

	// Pass four: a shared ISRC always wins, even across differing metadata.
	firstByISRC := make(map[string]int)
	for i, t := range tracks {
		isrc := NormalizeISRC(t.ISRC)
		if isrc == "" {
			continue
		}
		if prev, ok := firstByISRC[isrc]; ok {
			uf.union(prev, i)
			continue
		}
		firstByISRC[isrc] = i
	}

	// Collect the members of every group in input order.
	order := make([]int, 0, len(tracks))
	members := make(map[int][]int, len(tracks))
	for i := range tracks {
		root := uf.find(i)
		if _, seen := members[root]; !seen {
			order = append(order, root)
		}
		members[root] = append(members[root], i)
	}

	groups := make([]Group, 0, len(order))
	for _, root := range order {
		idx := members[root]
		best := idx[0]
		for _, i := range idx[1:] {
			if preferAsRepresentative(tracks[i], tracks[best]) {
				best = i
			}
		}
		g := Group{Track: tracks[best], Key: keys[best], Reason: ReasonUnique}
		if len(idx) > 1 {
			g.Reason = mergeReason(tracks, idx)
			for _, i := range idx {
				if i != best {
					g.Duplicates = append(g.Duplicates, tracks[i])
				}
			}
		}
		groups = append(groups, g)
	}
	return groups
}

// mergeReason reports why the members of a group belong together: a shared
// track ID or shared ISRC takes precedence over agreeing metadata.
func mergeReason(tracks []music.Track, idx []int) MergeReason {
	seenID := make(map[string]struct{}, len(idx))
	seenSource := make(map[string]struct{}, len(idx))
	seenISRC := make(map[string]struct{}, len(idx))
	for _, i := range idx {
		id := strings.TrimSpace(tracks[i].ID)
		if id != "" {
			if _, dup := seenID[id]; dup {
				return ReasonTrackID
			}
			seenID[id] = struct{}{}
		}
		if tracks[i].SourceProvider != "" && tracks[i].SourceID != "" {
			src := tracks[i].SourceProvider + ":" + tracks[i].SourceID
			if _, dup := seenSource[src]; dup {
				return ReasonTrackID
			}
			seenSource[src] = struct{}{}
		}
		isrc := NormalizeISRC(tracks[i].ISRC)
		if isrc != "" {
			if _, dup := seenISRC[isrc]; dup {
				return ReasonISRC
			}
			seenISRC[isrc] = struct{}{}
		}
	}
	return ReasonMetadata
}

// DeduplicateTracks is the convenience form of Deduplicate that only returns
// the representative tracks.
func DeduplicateTracks(tracks []music.Track, opts DedupOptions) []music.Track {
	groups := Deduplicate(tracks, opts)
	out := make([]music.Track, 0, len(groups))
	for _, g := range groups {
		out = append(out, g.Track)
	}
	return out
}

// IdentityKey builds the persistent identity key of a track: primary artist,
// normalised base title and version markers. It is stable across releases and
// providers and is what the library uses to recognise a track it already owns.
func IdentityKey(t music.Track) string {
	info := matcher.Analyze(t.Title)
	return strings.Join([]string{
		matcher.NormalizeArtist(music.PrimaryArtist(t.Artists)),
		info.Base,
		info.Versions.String(),
	}, "\x1f")
}

// titleBucket groups tracks that could describe the same recording before the
// artist credits and the runtime are taken into account.
func titleBucket(t music.Track) string {
	info := matcher.Analyze(t.Title)
	return info.Base + "\x1f" + info.Versions.String()
}

// ArtistKeySet returns every normalised artist credited for a track, including
// the featured artists named in the title.
func ArtistKeySet(t music.Track) map[string]struct{} {
	out := make(map[string]struct{}, len(t.Artists)+1)
	for _, name := range t.Artists {
		if k := matcher.NormalizeArtist(name); k != "" {
			out[k] = struct{}{}
		}
	}
	for _, name := range matcher.Analyze(t.Title).Featured {
		if k := matcher.NormalizeArtist(name); k != "" {
			out[k] = struct{}{}
		}
	}
	return out
}

func intersects(a, b map[string]struct{}) bool {
	if len(a) == 0 || len(b) == 0 {
		// Without any credit on one side the title, version and runtime
		// agreement is all the evidence there is, and it is accepted.
		return true
	}
	small, large := a, b
	if len(large) < len(small) {
		small, large = large, small
	}
	for k := range small {
		if _, ok := large[k]; ok {
			return true
		}
	}
	return false
}

// NormalizeISRC strips formatting from an ISRC and returns the empty string
// when the value is not a well formed 12 character code.
func NormalizeISRC(s string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(s) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	out := b.String()
	if len(out) != 12 {
		return ""
	}
	return out
}

// durationsMatch reports whether two runtimes are close enough to belong to the
// same recording. An unknown runtime on either side is not treated as evidence
// against a merge, because artist, title and version already agree.
func durationsMatch(a, b, toleranceMS int) bool {
	if a <= 0 || b <= 0 {
		return true
	}
	diff := a - b
	if diff < 0 {
		diff = -diff
	}
	return diff <= toleranceMS
}

// releaseTypeRank orders release types by how well they represent the original
// recording. Album versions are preferred so that duplicates from compilations
// or singles do not decide where a track is filed.
func releaseTypeRank(t music.ReleaseType) int {
	switch t {
	case music.ReleaseAlbum:
		return 0
	case music.ReleaseEP:
		return 1
	case music.ReleaseSingle:
		return 2
	case music.ReleaseLive:
		return 3
	case music.ReleaseCompilation:
		return 4
	case music.ReleaseRemix:
		return 5
	default:
		return 6
	}
}

// preferAsRepresentative reports whether candidate should replace current as
// the representative of a group.
func preferAsRepresentative(candidate, current music.Track) bool {
	if r1, r2 := releaseTypeRank(candidate.ReleaseType), releaseTypeRank(current.ReleaseType); r1 != r2 {
		return r1 < r2
	}
	// Earlier release: closer to the original publication.
	switch {
	case candidate.Year > 0 && current.Year > 0 && candidate.Year != current.Year:
		return candidate.Year < current.Year
	case candidate.Year > 0 && current.Year == 0:
		return true
	}
	// A known ISRC makes later matching more reliable.
	if hasISRC1, hasISRC2 := NormalizeISRC(candidate.ISRC) != "", NormalizeISRC(current.ISRC) != ""; hasISRC1 != hasISRC2 {
		return hasISRC1
	}
	if candidate.DiscNumber != current.DiscNumber && candidate.DiscNumber > 0 && current.DiscNumber > 0 {
		return candidate.DiscNumber < current.DiscNumber
	}
	if candidate.TrackNumber != current.TrackNumber && candidate.TrackNumber > 0 && current.TrackNumber > 0 {
		return candidate.TrackNumber < current.TrackNumber
	}
	return false
}

// SortGroups orders groups the way they should be processed: by album artist,
// year, release, disc and track number.
func SortGroups(groups []Group) {
	sort.SliceStable(groups, func(i, j int) bool {
		a, b := groups[i].Track, groups[j].Track
		if a.Year != b.Year {
			return a.Year < b.Year
		}
		if a.Album != b.Album {
			return a.Album < b.Album
		}
		if a.DiscNumber != b.DiscNumber {
			return a.DiscNumber < b.DiscNumber
		}
		if a.TrackNumber != b.TrackNumber {
			return a.TrackNumber < b.TrackNumber
		}
		return a.Title < b.Title
	})
}

// unionFind is a small disjoint set structure with path compression.
type unionFind struct {
	parent []int
	rank   []int
}

func newUnionFind(n int) *unionFind {
	uf := &unionFind{parent: make([]int, n), rank: make([]int, n)}
	for i := range uf.parent {
		uf.parent[i] = i
	}
	return uf
}

func (u *unionFind) find(i int) int {
	for u.parent[i] != i {
		u.parent[i] = u.parent[u.parent[i]]
		i = u.parent[i]
	}
	return i
}

func (u *unionFind) union(a, b int) {
	ra, rb := u.find(a), u.find(b)
	if ra == rb {
		return
	}
	if u.rank[ra] < u.rank[rb] {
		ra, rb = rb, ra
	}
	u.parent[rb] = ra
	if u.rank[ra] == u.rank[rb] {
		u.rank[ra]++
	}
}

// EquivalentReleases reports whether two releases represent the same effective
// release and will produce the same canonical library layout.
//
// Equivalence requires:
//  1. Same canonical release directory (identical album artist, release title, year, and release type suffix).
//  2. Same release type.
//  3. Same non-zero track count.
//  4. In order by (disc, track number), every track has the same disc number, track number,
//     same canonical track filename, and non-conflicting ISRCs.
func EquivalentReleases(r1 music.Release, tracks1 []music.Track, r2 music.Release, tracks2 []music.Track) bool {
	if storage.ReleaseDirRel(r1) != storage.ReleaseDirRel(r2) {
		return false
	}
	if r1.ReleaseType != r2.ReleaseType {
		return false
	}
	if len(tracks1) != len(tracks2) || len(tracks1) == 0 {
		return false
	}

	t1 := sortTracksByPosition(tracks1)
	t2 := sortTracksByPosition(tracks2)

	for i := range t1 {
		d1, d2 := t1[i].DiscNumber, t2[i].DiscNumber
		if d1 <= 0 {
			d1 = 1
		}
		if d2 <= 0 {
			d2 = 1
		}
		if d1 != d2 {
			return false
		}
		if t1[i].TrackNumber != t2[i].TrackNumber {
			return false
		}
		if storage.TrackFileName(t1[i], "") != storage.TrackFileName(t2[i], "") {
			return false
		}
		isrc1 := NormalizeISRC(t1[i].ISRC)
		isrc2 := NormalizeISRC(t2[i].ISRC)
		if isrc1 != "" && isrc2 != "" && isrc1 != isrc2 {
			return false
		}
	}
	return true
}

func sortTracksByPosition(tracks []music.Track) []music.Track {
	out := append([]music.Track(nil), tracks...)
	sort.SliceStable(out, func(i, j int) bool {
		d1, d2 := out[i].DiscNumber, out[j].DiscNumber
		if d1 <= 0 {
			d1 = 1
		}
		if d2 <= 0 {
			d2 = 1
		}
		if d1 != d2 {
			return d1 < d2
		}
		if out[i].TrackNumber != out[j].TrackNumber {
			return out[i].TrackNumber < out[j].TrackNumber
		}
		return out[i].Title < out[j].Title
	})
	return out
}

// DeduplicateReleases filters out equivalent releases that would target the same
// canonical library paths with equivalent track listings, preserving the first seen representative.
func DeduplicateReleases(releases []music.Release, releaseTracks [][]music.Track) ([]music.Release, [][]music.Track) {
	if len(releases) == 0 {
		return nil, nil
	}
	outReleases := make([]music.Release, 0, len(releases))
	outTracks := make([][]music.Track, 0, len(releases))
	for i, r := range releases {
		var ts []music.Track
		if i < len(releaseTracks) {
			ts = releaseTracks[i]
		}
		dup := false
		for j, prev := range outReleases {
			if EquivalentReleases(prev, outTracks[j], r, ts) {
				dup = true
				break
			}
		}
		if !dup {
			outReleases = append(outReleases, r)
			outTracks = append(outTracks, ts)
		}
	}
	return outReleases, outTracks
}
