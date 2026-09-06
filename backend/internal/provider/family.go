package provider

import "strings"

// Family represents a platform family grouping one or more providers that share
// the same upstream platform, quota, and authentication infrastructure.
type Family string

const (
	FamilyYouTube Family = "youtube"
)

// FamilyOf returns the platform family for a given provider name.
// Both "youtube" and "ytmusic" belong to FamilyYouTube.
func FamilyOf(providerName string) Family {
	switch strings.ToLower(strings.TrimSpace(providerName)) {
	case "youtube", "ytmusic":
		return FamilyYouTube
	default:
		return Family(strings.ToLower(strings.TrimSpace(providerName)))
	}
}

// FamilyProvider is an optional interface providers can implement to declare
// their platform family explicitly.
type FamilyProvider interface {
	Family() Family
}
