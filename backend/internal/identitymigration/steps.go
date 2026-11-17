package identitymigration

import (
	"ytdm/backend/internal/discography"
	"ytdm/backend/internal/music"
)

// Registered lists every identity change in order. Raising
// discography.IdentityScheme means appending the step that brings the rows of
// the previous scheme over; New refuses a list with a gap.
var Registered = []Step{
	{
		From: discography.IdentitySchemeUnrecorded,
		To:   discography.RemixReadingScheme,
		Name: "unbracketed-remix-title",
		// Versions up to 1.3.0 took every word before a trailing "remix" as
		// the remixer ("Blinding Lights Major Lazer Remix" became
		// "blinding"). Rows they wrote are recomputed from their stored
		// title and artists with the current reading.
		Recompute: func(row Row) Outcome {
			track := music.Track{Title: row.Title, Artists: row.Artists, ReleaseType: row.ReleaseType}
			key, verdict := discography.RereadRemixTitleKey(track, row.Key)
			switch verdict {
			case discography.KeyCurrent:
				return Keep()
			case discography.KeyRewrite:
				return Rewrite(key)
			default:
				return HoldRow(HoldNotDerivable)
			}
		},
	},
}
