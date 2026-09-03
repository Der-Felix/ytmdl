package library

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/database/repository"
	"ytdm/backend/internal/jobs"
	"ytdm/backend/internal/music"
)

// CompatKind classifies one deviation between the library on disk and the
// layout v0.9.0 produces.
type CompatKind string

const (
	// CompatArtistFolder means the release sits under a joined credit string
	// rather than under its primary artist, which splits one artist into
	// several in Plex, Jellyfin and Emby.
	CompatArtistFolder CompatKind = "artist_folder"
	// CompatMultiDiscName means the file still uses the old "D-NN" prefix
	// instead of the "DNN" form Plex documents.
	CompatMultiDiscName CompatKind = "multidisc_name"
	// CompatMissingTotals means TRACKTOTAL or DISCTOTAL is absent.
	CompatMissingTotals CompatKind = "missing_totals"
	// CompatMissingLyrics means no sidecar exists and none has been looked for.
	CompatMissingLyrics CompatKind = "missing_lyrics"
)

// CompatIssue is one proposed change. It is a proposal, never an action.
type CompatIssue struct {
	ID      string     `json:"id"`
	Kind    CompatKind `json:"kind"`
	TrackID string     `json:"track_id"`
	Title   string     `json:"title"`
	From    string     `json:"from"`
	To      string     `json:"to,omitempty"`
	Detail  string     `json:"detail,omitempty"`
}

// CompatReport is the result of a compatibility check.
type CompatReport struct {
	FilesScanned int           `json:"files_scanned"`
	Issues       []CompatIssue `json:"issues"`
	Warnings     []string      `json:"warnings,omitempty"`
}

// CompatibilityReport compares the library against the layout this version
// produces. It is a strict dry run: it never renames, moves or writes
// anything. Applying a finding is a separate, explicitly confirmed call.
func (s *Service) CompatibilityReport(ctx context.Context) (*CompatReport, error) {
	stored, err := s.catalog.ListAllTracks(ctx)
	if err != nil {
		return nil, err
	}

	report := &CompatReport{}
	for _, entry := range stored {
		track := entry.Track
		files, err := s.files.ListByTrack(ctx, track.ID)
		if err != nil {
			report.Warnings = append(report.Warnings, track.Title+": "+err.Error())
			continue
		}
		for _, file := range files {
			report.FilesScanned++
			s.appendCompatIssues(report, track, entry, file)
		}
	}
	s.latestCompat.Store(report)
	return report, nil
}

// appendCompatIssues records every deviation of one library file.
func (s *Service) appendCompatIssues(report *CompatReport, track music.Track,
	entry repository.StoredTrack, file music.File) {

	release := releaseFromStored(entry)
	want, err := s.library.Layout().TrackPath(release, track, filepath.Ext(file.Path))
	if err == nil {
		wantRel := s.library.RelPath(want)
		if wantRel != file.Path {
			kind := CompatArtistFolder
			if filepath.Dir(wantRel) == filepath.Dir(file.Path) {
				kind = CompatMultiDiscName
			}
			report.Issues = append(report.Issues, CompatIssue{
				ID:      music.NewID(),
				Kind:    kind,
				TrackID: track.ID,
				Title:   track.Label(),
				From:    file.Path,
				To:      wantRel,
			})
		}
	}

	if track.TrackTotal <= 0 || track.DiscTotal <= 0 {
		report.Issues = append(report.Issues, CompatIssue{
			ID:      music.NewID(),
			Kind:    CompatMissingTotals,
			TrackID: track.ID,
			Title:   track.Label(),
			From:    file.Path,
			Detail:  "TRACKTOTAL or DISCTOTAL is missing. Retagging the track fixes it.",
		})
	}

	if track.DisplayLyricsState() == music.LyricsUnknown {
		abs, _, confErr := VerifyPathConfinement(s.library.Root(), file.Path, false)
		if confErr == nil {
			if path, _, readErr := s.library.ReadLyrics(abs); readErr == nil && path == "" {
				report.Issues = append(report.Issues, CompatIssue{
					ID:      music.NewID(),
					Kind:    CompatMissingLyrics,
					TrackID: track.ID,
					Title:   track.Label(),
					From:    file.Path,
					Detail:  "No lyrics have been looked for yet.",
				})
			}
		}
	}
}

// releaseFromStored rebuilds the release a stored track belongs to, so the
// wanted path can be computed with the same layout the downloader uses.
func releaseFromStored(entry repository.StoredTrack) music.Release {
	title := strings.TrimSpace(entry.Track.Album)
	if title == "" {
		title = entry.Track.DisplayTitle()
	}
	releaseType := entry.Track.ReleaseType
	if releaseType == "" {
		releaseType = music.ReleaseAlbum
	}
	albumArtist := music.ResolveAlbumArtist(entry.Track.AlbumArtist, entry.Track.Artists)
	if albumArtist == music.UnknownArtist {
		albumArtist = entry.Track.DisplayAlbumArtist()
	}
	return music.Release{
		Title:       title,
		AlbumArtist: albumArtist,
		Artists:     entry.Track.Artists,
		ReleaseType: releaseType,
		Year:        entry.Track.Year,
	}
}

// ReorganizeRequest asks for a set of compatibility findings to be applied.
type ReorganizeRequest struct {
	// Confirm has to be true. The field exists so that a reorganise can never
	// be triggered by an accidental or replayed request: moving a user's
	// library is not something the server does on its own initiative.
	Confirm  bool     `json:"confirm"`
	IssueIDs []string `json:"issue_ids"`
}

// ReorganizeResult reports what was applied.
type ReorganizeResult struct {
	Requested int      `json:"requested"`
	Moved     int      `json:"moved"`
	Skipped   int      `json:"skipped"`
	Warnings  []string `json:"warnings,omitempty"`
}

// Reorganize applies the selected compatibility findings.
//
// It moves nothing that was not explicitly listed, refuses to overwrite an
// existing file, and moves the lyrics sidecar together with its audio so the
// two can never end up in different directories. Nothing here ever runs
// automatically — not at start up, not after a scan.
func (s *Service) Reorganize(ctx context.Context, req ReorganizeRequest) (*ReorganizeResult, error) {
	if !req.Confirm {
		return nil, apperr.New(apperr.CodeInvalidRequest,
			"A reorganize has to be confirmed explicitly.")
	}
	if len(req.IssueIDs) == 0 {
		return nil, apperr.New(apperr.CodeInvalidRequest, "No findings were selected.")
	}

	report := s.latestCompat.Load()
	if report == nil {
		return nil, apperr.New(apperr.CodeInvalidRequest,
			"Run a compatibility check first; there is no report to apply.")
	}

	wanted := make(map[string]struct{}, len(req.IssueIDs))
	for _, id := range req.IssueIDs {
		wanted[id] = struct{}{}
	}

	result := &ReorganizeResult{Requested: len(req.IssueIDs)}
	for _, issue := range report.Issues {
		if _, selected := wanted[issue.ID]; !selected {
			continue
		}
		if issue.To == "" || issue.From == issue.To {
			result.Skipped++
			continue
		}
		if err := s.applyMove(ctx, issue); err != nil {
			result.Skipped++
			result.Warnings = append(result.Warnings, issue.Title+": "+apperr.MessageOf(err))
			continue
		}
		result.Moved++
	}
	return result, nil
}

// applyMove relocates one library file and its sidecar, then records the new
// path. The catalogue is updated only after the move succeeded, so a failure
// can never leave a row pointing at a file that is not there.
func (s *Service) applyMove(ctx context.Context, issue CompatIssue) error {
	unlock := s.locks.Lock("track:" + issue.TrackID)
	defer unlock()

	if s.jobs != nil {
		busy, err := s.jobs.HasUnfinishedJob(ctx, jobs.TypeTrack, issue.TrackID)
		if err != nil {
			return err
		}
		if busy {
			return apperr.New(apperr.CodeAlreadyExists,
				"Cannot move the track while a download job is active.")
		}
	}

	source, _, err := VerifyPathConfinement(s.library.Root(), issue.From, false)
	if err != nil {
		return err
	}
	destination, _, err := VerifyPathConfinement(s.library.Root(), issue.To, true)
	if err != nil {
		return err
	}
	if s.library.Exists(destination) {
		return apperr.Newf(apperr.CodeAlreadyExists,
			"A file already exists at %q; the move was not carried out.", issue.To)
	}

	if err := s.library.MoveTrack(source, destination); err != nil {
		return err
	}

	stored, err := s.files.FindByPath(ctx, issue.From)
	if err != nil {
		return err
	}
	if stored != nil {
		_ = s.files.DeleteByPath(ctx, issue.From)
		stored.Path = s.library.RelPath(destination)
		if _, err := s.files.Upsert(ctx, *stored); err != nil {
			return err
		}
	}

	// An empty source directory left behind would show up as an empty artist
	// in every media server. os.Remove only succeeds when it is truly empty.
	_ = os.Remove(filepath.Dir(source))
	_ = os.Remove(filepath.Dir(filepath.Dir(source)))
	return nil
}
