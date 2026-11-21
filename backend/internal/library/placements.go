package library

import (
	"context"
	"path/filepath"

	"ytdm/backend/internal/music"
)

// listPlacements returns every placement, or none when the service has no
// placement store.
func (s *Service) listPlacements(ctx context.Context) ([]music.Placement, error) {
	if s.placements == nil {
		return nil, nil
	}
	return s.placements.ListAll(ctx)
}

// withoutPlacements drops the files on disk that are placements. A placement
// is a catalogued copy of a recording on a further release: it is neither an
// orphan nor a file of its recording, so the scans that compare the disk with
// the files table must not see it at all - not as untracked, not as a
// duplicate, and not as something to move or adopt.
func withoutPlacements(discovered []DiscoveredFile, placements []music.Placement) []DiscoveredFile {
	if len(placements) == 0 {
		return discovered
	}
	placed := make(map[string]struct{}, len(placements))
	for _, p := range placements {
		placed[filepath.Clean(p.Path)] = struct{}{}
	}
	kept := make([]DiscoveredFile, 0, len(discovered))
	for _, df := range discovered {
		if _, ok := placed[filepath.Clean(df.RelPath)]; !ok {
			kept = append(kept, df)
		}
	}
	return kept
}
