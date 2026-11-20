package music

import "regexp"

// liveTitleRe matches the ways a release title names itself a live recording:
// "(Live)", "[Live at ...]", "Live from CMA Fest", "MTV Unplugged", "In
// Concert", "Song - Live", "Live 2007" or a release simply called "Live".
//
// A bare "live" anywhere in the title is deliberately not enough. Since the
// marker now also decides the type of singles and EPs, "Long Live", "Live
// Forever" or "Live Your Life" must keep the type the provider reported.
var liveTitleRe = regexp.MustCompile(`(?i)(` +
	`[(\[]\s*live\b` +
	`|\blive (at|in|from|on|aus|im)\b` +
	`|\blive album\b` +
	`|\bunplugged\b` +
	`|\bin concert\b` +
	`|\s[-–—]\s*live\b` +
	`|\blive (19|20)\d{2}\b` +
	`|^\s*live\s*$` +
	`)`)

// remixTitleRe matches "Remix", "Remixes" and "Remixed" as whole words, which
// covers "(Oliver Heldens Remix)", "(Remixes)", "Remixes #2", "(The Remixes)"
// and "Extended Remixes". A word that merely contains the letters — "Premix",
// "Remixer" — is not a marker.
var remixTitleRe = regexp.MustCompile(`(?i)\bremix(es|ed)?\b`)

// TitleReleaseType reports the release type a release title asserts by
// itself: ReleaseLive for a live marker, ReleaseRemix for a remix marker.
// ok is false when the title carries neither.
//
// The providers only know album, single, EP and compilation. A remix single
// or a live EP is still a remix or a live release, so every metadata
// provider lets this marker win over its own single, EP or album type; only
// a compilation the provider itself declared keeps that type. Live wins over
// remix because a live remix set is first of all a live recording.
func TitleReleaseType(title string) (kind ReleaseType, ok bool) {
	switch {
	case liveTitleRe.MatchString(title):
		return ReleaseLive, true
	case remixTitleRe.MatchString(title):
		return ReleaseRemix, true
	default:
		return "", false
	}
}
