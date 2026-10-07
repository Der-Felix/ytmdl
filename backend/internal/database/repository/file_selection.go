package repository

import (
	"strconv"

	"ytdm/backend/internal/music"
)

// Prefer a measured, compatible recording over unknown or incompatible files.
// Retained originals remain available by file ID and in the track's file list.
func fileDurationRankSQL(expected, measured string) string {
	return `CASE WHEN ` + expected + ` <= 0 THEN 0
		WHEN ` + measured + ` <= 0 THEN 1
		WHEN abs(` + measured + `::bigint - ` + expected + `::bigint) <= ` + strconv.Itoa(music.RecordingDurationToleranceMS) + ` THEN 0
		ELSE 2 END`
}

// Library rows describe recordings, not every physical copy of a recording.
var primaryFileJoin = `LEFT JOIN LATERAL (
	SELECT candidate.* FROM files candidate WHERE candidate.track_id = t.id
	ORDER BY ` + fileDurationRankSQL("t.duration_ms", "candidate.duration_ms") + `, candidate.path
	LIMIT 1
) f ON true`
