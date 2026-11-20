package music

import "testing"

func TestTitleReleaseType(t *testing.T) {
	tests := []struct {
		title string
		want  ReleaseType // "" means no marker
	}{
		// Remix markers.
		{"I'm Good (Blue) (Oliver Heldens Remix)", ReleaseRemix},
		{"Say My Name (Remixes)", ReleaseRemix},
		{"Say My Name (Remixes #2)", ReleaseRemix},
		{"Remixes #2", ReleaseRemix},
		{"Expectations (The Remixes)", ReleaseRemix},
		{"Meant To Be (Extended Remixes)", ReleaseRemix},
		{"Deep In Your Love (Dimitri Vegas & Like Mike, Ben Nicky & Dr Phunk Remix)", ReleaseRemix},
		{"In The Name Of Love Remixes", ReleaseRemix},
		{"Human After All: Remixed", ReleaseRemix},
		{"[REMIX]", ReleaseRemix},

		// Live markers.
		{"Meant To Be (Live From CMA Fest 2018)", ReleaseLive},
		{"Hello (Live)", ReleaseLive},
		{"Hello [Live at the BBC]", ReleaseLive},
		{"Live at Wembley", ReleaseLive},
		{"MTV Unplugged", ReleaseLive},
		{"Hello - Live", ReleaseLive},
		{"Live 2007", ReleaseLive},
		{"Live", ReleaseLive},
		{"Elvis In Concert", ReleaseLive},
		{"Shout (Live Remix)", ReleaseLive},

		// No marker.
		{"Heart Still Beating (Extended)", ""},
		{"I'm Good (Blue) (Acoustic)", ""},
		{"I'm Good (Blue)", ""},
		{"Premixed", ""},
		{"The Remixer", ""},
		{"Remixology", ""},
		{"Long Live", ""},
		{"Live Your Life", ""},
		{"Live Forever", ""},
		{"Alive", ""},
		{"Oliver Heldens Mix", ""},
		{"", ""},
	}
	for _, tc := range tests {
		got, ok := TitleReleaseType(tc.title)
		if tc.want == "" {
			if ok {
				t.Errorf("TitleReleaseType(%q) = %q, want no marker", tc.title, got)
			}
			continue
		}
		if !ok || got != tc.want {
			t.Errorf("TitleReleaseType(%q) = %q, %v, want %q", tc.title, got, ok, tc.want)
		}
	}
}
