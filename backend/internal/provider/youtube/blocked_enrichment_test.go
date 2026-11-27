package youtube

import (
	"context"
	"testing"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/music"
	"ytdm/backend/internal/ytdlp"
)

// flatSearchWithoutRuntime answers the flat search with a candidate that
// carries neither runtime nor credits; only its extraction would add them.
const flatSearchWithoutRuntime = ` *ytsearch*)
 echo '{"id":"aaaaaaaaaaa","title":"Song (Lyrics)","channel":"Some Lyrics Channel","url":"https://www.youtube.com/watch?v=aaaaaaaaaaa"}'
 ;;`

// A bot challenge on the enrichment used to be swallowed: the candidate kept
// no runtime and no credits, scored 0.0, and the item failed for good with
// MATCH_FAILED. The refusal is the search's answer now.
func TestBotChallengeDuringEnrichmentIsTheSearchAnswer(t *testing.T) {
	binary := offlineYTDLP(t, "case \"$*\" in\n"+flatSearchWithoutRuntime+`
 *watch?v=aaaaaaaaaaa*)
 echo "ERROR: [youtube] aaaaaaaaaaa: Sign in to confirm you are not a bot" >&2
 exit 1;;
 *) exit 7;;
esac
`)
	p, err := New(Config{Name: ProviderName, Client: ytdlp.New(ytdlp.Options{Binary: binary, DisableQueryCache: true})})
	if err != nil {
		t.Fatal(err)
	}
	track := music.Track{Title: "Song", Artists: []string{"Artist"}, AlbumArtist: "Artist", DurationMS: 200000}

	candidates, err := p.Search(context.Background(), track)
	if apperr.CodeOf(err) != apperr.CodeSessionBotChallenge {
		t.Fatalf("search = %v, %v; want the bot challenge", candidates, err)
	}
}

// A timeout on the enrichment stays best effort: the candidate is returned
// without a runtime and scored cautiously, as before.
func TestEnrichmentTimeoutStaysBestEffort(t *testing.T) {
	binary := offlineYTDLP(t, "case \"$*\" in\n"+flatSearchWithoutRuntime+`
 *watch?v=aaaaaaaaaaa*)
 echo "ERROR: [youtube] aaaaaaaaaaa: Read timed out" >&2
 exit 1;;
 *) exit 7;;
esac
`)
	p, err := New(Config{Name: ProviderName, Client: ytdlp.New(ytdlp.Options{Binary: binary, DisableQueryCache: true})})
	if err != nil {
		t.Fatal(err)
	}
	track := music.Track{Title: "Song", Artists: []string{"Artist"}, DurationMS: 200000}

	candidates, err := p.Search(context.Background(), track)
	if err != nil || len(candidates) != 1 || candidates[0].DurationMS != 0 {
		t.Fatalf("search = %+v, %v; want the candidate without runtime", candidates, err)
	}
}
