// Package metadata writes tags and cover art onto finished audio files. Opus
// and Ogg Vorbis files are tagged natively by rewriting their comment header;
// every other container is remuxed with ffmpeg using a stream copy. In both
// cases the audio samples stay exactly as they were downloaded.
package metadata

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/ffmpeg"
	"ytdm/backend/internal/music"
	"ytdm/backend/internal/provider"
)

// Vorbis comment field names written by the backend.
const (
	FieldTitle       = "TITLE"
	FieldArtist      = "ARTIST"
	FieldAlbum       = "ALBUM"
	FieldAlbumArtist = "ALBUMARTIST"
	FieldTrackNumber = "TRACKNUMBER"
	FieldTrackTotal  = "TRACKTOTAL"
	FieldDiscNumber  = "DISCNUMBER"
	FieldDiscTotal   = "DISCTOTAL"
	FieldDate        = "DATE"
	FieldISRC        = "ISRC"
	FieldCompilation = "COMPILATION"
	FieldSource      = "SOURCE"
	FieldSourceID    = "SOURCE_ID"
	FieldSourceURL   = "SOURCE_URL"
	FieldPicture     = "METADATA_BLOCK_PICTURE"
)

// Tags is the set of values written onto a file.
type Tags struct {
	Title       string
	Artists     []string
	Album       string
	AlbumArtist string

	TrackNumber int
	TrackTotal  int
	DiscNumber  int
	DiscTotal   int

	Date string
	ISRC string

	// Compilation marks a release whose tracks are by different artists. Plex
	// keys on the literal "Various Artists" album artist, Emby and
	// iTunes-style clients on this flag.
	Compilation bool

	Source    string
	SourceID  string
	SourceURL string
}

// TagsFor builds the tag set of a track that was downloaded from source.
//
// The disc numbers are normalised, the track total is not. Every audio file
// sits on at least disc one of at least one disc, so DISCNUMBER=1 and
// DISCTOTAL=1 are facts rather than guesses and all three media servers show
// them. How many tracks a release has, on the other hand, is knowledge only
// the provider has: when it delivered none, the tag stays absent instead of
// being filled with a plausible number.
func TagsFor(track music.Track, source provider.MediaSource) Tags {
	discNumber := track.DiscNumber
	if discNumber <= 0 {
		discNumber = 1
	}
	discTotal := track.DiscTotal
	if discTotal < discNumber {
		discTotal = discNumber
	}

	tags := Tags{
		Title:       track.DisplayTitle(),
		Artists:     track.Artists,
		Album:       strings.TrimSpace(track.Album),
		AlbumArtist: track.DisplayAlbumArtist(),
		TrackNumber: track.TrackNumber,
		TrackTotal:  track.TrackTotal,
		DiscNumber:  discNumber,
		DiscTotal:   discTotal,
		ISRC:        strings.TrimSpace(track.ISRC),
		Compilation: track.Compilation || strings.EqualFold(track.DisplayAlbumArtist(), music.VariousArtists),
		Source:      source.Provider,
		SourceID:    source.ID,
		SourceURL:   source.URL,
	}
	if len(tags.Artists) == 0 {
		tags.Artists = []string{music.UnknownArtist}
	}
	if track.Year > 0 {
		tags.Date = strconv.Itoa(track.Year)
	}
	return tags
}

// Comments renders the tags as Vorbis comment entries. One ARTIST entry is
// written per credited artist, which is how Vorbis comments express multiple
// values.
func (t Tags) Comments() []string {
	out := make([]string, 0, 16)

	add := func(key, value string) {
		if strings.TrimSpace(value) == "" {
			return
		}
		out = append(out, formatComment(key, value))
	}
	addNumber := func(key string, value int) {
		if value <= 0 {
			return
		}
		out = append(out, formatComment(key, strconv.Itoa(value)))
	}

	add(FieldTitle, t.Title)
	for _, artist := range t.Artists {
		add(FieldArtist, strings.TrimSpace(artist))
	}
	add(FieldAlbum, t.Album)
	add(FieldAlbumArtist, t.AlbumArtist)
	addNumber(FieldTrackNumber, t.TrackNumber)
	addNumber(FieldTrackTotal, t.TrackTotal)
	addNumber(FieldDiscNumber, t.DiscNumber)
	addNumber(FieldDiscTotal, t.DiscTotal)
	add(FieldDate, t.Date)
	add(FieldISRC, t.ISRC)
	if t.Compilation {
		out = append(out, formatComment(FieldCompilation, "1"))
	}
	add(FieldSource, t.Source)
	add(FieldSourceID, t.SourceID)
	add(FieldSourceURL, t.SourceURL)

	return out
}

// ffmpegMetadata renders the tags as ffmpeg -metadata arguments.
func (t Tags) ffmpegMetadata() []string {
	out := make([]string, 0, 24)
	add := func(key, value string) {
		if strings.TrimSpace(value) == "" {
			return
		}
		out = append(out, "-metadata", key+"="+value)
	}

	add("title", t.Title)
	add("artist", music.JoinArtists(t.Artists))
	add("album", t.Album)
	add("album_artist", t.AlbumArtist)
	if t.TrackNumber > 0 {
		if t.TrackTotal > 0 {
			add("track", fmt.Sprintf("%d/%d", t.TrackNumber, t.TrackTotal))
		} else {
			add("track", strconv.Itoa(t.TrackNumber))
		}
	}
	if t.DiscNumber > 0 {
		if t.DiscTotal > 0 {
			add("disc", fmt.Sprintf("%d/%d", t.DiscNumber, t.DiscTotal))
		} else {
			add("disc", strconv.Itoa(t.DiscNumber))
		}
	}
	add("date", t.Date)
	add("ISRC", t.ISRC)
	if t.Compilation {
		add("compilation", "1")
	}
	add("SOURCE", t.Source)
	add("SOURCE_ID", t.SourceID)
	add("SOURCE_URL", t.SourceURL)
	return out
}

// Tagger writes tags onto audio files.
type Tagger struct {
	ffmpeg *ffmpeg.Runner
}

// NewTagger builds a tagger. The ffmpeg runner is only used for containers
// that have no native writer.
func NewTagger(runner *ffmpeg.Runner) *Tagger {
	return &Tagger{ffmpeg: runner}
}

// Apply writes tags and, when artwork is given, an embedded cover onto the
// file at path. The file is replaced atomically.
func (t *Tagger) Apply(ctx context.Context, path string, tags Tags, artwork *Artwork) error {
	if strings.TrimSpace(path) == "" {
		return apperr.New(apperr.CodeInternal, "The file to tag was not given.")
	}
	if _, err := os.Stat(path); err != nil {
		return apperr.Wrap(apperr.CodeTaggingFailed, "The file to tag does not exist.", err)
	}

	switch strings.ToLower(filepath.Ext(path)) {
	case ".opus", ".ogg", ".oga":
		return t.applyVorbis(ctx, path, tags, artwork)
	default:
		return t.applyFFmpeg(ctx, path, tags, artwork)
	}
}

// UpdateArtwork replaces or removes the embedded cover art on an audio file,
// preserving all existing audio content and existing tags without re-encoding.
func (t *Tagger) UpdateArtwork(ctx context.Context, path string, artwork *Artwork) error {
	if strings.TrimSpace(path) == "" {
		return apperr.New(apperr.CodeInternal, "The file to tag was not given.")
	}
	if _, err := os.Stat(path); err != nil {
		return apperr.Wrap(apperr.CodeTaggingFailed, "The file to tag does not exist.", err)
	}

	switch strings.ToLower(filepath.Ext(path)) {
	case ".opus", ".ogg", ".oga":
		return UpdateOpusArtwork(path, artwork)
	default:
		return t.applyFFmpeg(ctx, path, Tags{}, artwork)
	}
}

// applyVorbis rewrites the Ogg comment header in place. No audio page is
// touched, so this is the lossless path for native Opus.
func (t *Tagger) applyVorbis(_ context.Context, path string, tags Tags, artwork *Artwork) error {
	comments := tags.Comments()
	if artwork != nil {
		comments = append(comments, formatComment(FieldPicture, artwork.MetadataBlockPicture()))
	}
	return writeOpusComments(path, comments)
}

// applyFFmpeg remuxes the file with a stream copy so that the container's own
// metadata structures can be written. The samples are copied unchanged.
func (t *Tagger) applyFFmpeg(ctx context.Context, path string, tags Tags, artwork *Artwork) error {
	if t.ffmpeg == nil {
		return apperr.New(apperr.CodeTaggingFailed,
			"This container needs ffmpeg for tagging, but no ffmpeg runner is configured.")
	}

	dir := filepath.Dir(path)
	ext := filepath.Ext(path)

	temp, err := os.CreateTemp(dir, ".ytdm-tag-*"+ext)
	if err != nil {
		return apperr.Wrap(apperr.CodeTaggingFailed, "The temporary file could not be created.", err)
	}
	tempPath := temp.Name()
	temp.Close()
	defer os.Remove(tempPath)

	args := []string{"-i", path}

	var coverPath string
	if artwork != nil && supportsEmbeddedCover(ext) {
		coverPath = filepath.Join(dir, ".ytdm-cover"+artwork.Extension())
		if err := os.WriteFile(coverPath, artwork.Data, 0o600); err != nil {
			return apperr.Wrap(apperr.CodeTaggingFailed, "The cover could not be written.", err)
		}
		defer os.Remove(coverPath)
		args = append(args, "-i", coverPath,
			"-map", "0:a", "-map", "1:v",
			"-c", "copy",
			"-disposition:v:0", "attached_pic")
	} else {
		args = append(args, "-map", "0:a", "-c", "copy")
	}

	args = append(args, tags.ffmpegMetadata()...)
	args = append(args, tempPath)

	if err := t.ffmpeg.Run(ctx, apperr.CodeTaggingFailed, args...); err != nil {
		return err
	}

	info, statErr := os.Stat(path)
	if statErr == nil {
		_ = os.Chmod(tempPath, info.Mode().Perm())
	}
	if err := os.Rename(tempPath, path); err != nil {
		return apperr.Wrap(apperr.CodeTaggingFailed, "The tagged file could not replace the original.", err)
	}
	return nil
}

// supportsEmbeddedCover reports whether a container can carry an attached
// picture stream.
func supportsEmbeddedCover(ext string) bool {
	switch strings.ToLower(ext) {
	case ".m4a", ".mp4", ".mp3", ".flac":
		return true
	default:
		return false
	}
}

// ReadTags reads back the tags of an Ogg based file. It is used to verify what
// was written and in tests.
func ReadTags(path string) (map[string][]string, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".opus", ".ogg", ".oga":
		return readOpusComments(path)
	default:
		return nil, apperr.Newf(apperr.CodeUnsupportedMediaType,
			"Reading tags from %q files is not supported.", filepath.Ext(path))
	}
}
