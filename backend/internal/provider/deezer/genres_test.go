package deezer

import (
	"net/http"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Recorded from https://api.deezer.com/artist/27/albums?limit=100 (Daft Punk,
// 2026-11-24), trimmed to the fields genre derivation reads, and split into
// two pages to exercise the next link. Real counts: 27 Dance (113),
// 8 Electro (106), 2 Film/Videospiele (173), 1 Pop (132).
const fixtureArtistAlbumsPage1 = `{"data":[
{"id":494309801,"title":"Random Access Memories (Drumless Edition)","genre_id":106,"record_type":"album","type":"album"},{"id":294609352,"title":"Homework (25th Anniversary Edition)","genre_id":113,"record_type":"album","type":"album"},{"id":6575789,"title":"Random Access Memories","genre_id":113,"record_type":"album","type":"album"},{"id":438167857,"title":"Random Access Memories (10th Anniversary Edition)","genre_id":106,"record_type":"album","type":"album"},{"id":1471670,"title":"TRON: Legacy Reconfigured","genre_id":106,"record_type":"album","type":"album"},{"id":192529232,"title":"TRON: Legacy - The Complete Edition (Original Motion Picture Soundtrack)","genre_id":173,"record_type":"album","type":"album"},{"id":321108967,"title":"TRON: Legacy","genre_id":173,"record_type":"album","type":"album"},{"id":304193,"title":"Alive 2007","genre_id":113,"record_type":"album","type":"album"},{"id":1343199,"title":"Musique, Vol. 1","genre_id":106,"record_type":"album","type":"album"},{"id":8244118,"title":"Human After All (Remixes)","genre_id":113,"record_type":"album","type":"album"},{"id":303459,"title":"Human After All","genre_id":113,"record_type":"album","type":"album"},{"id":299205,"title":"Daft Club","genre_id":113,"record_type":"album","type":"album"},{"id":299137,"title":"Alive 1997","genre_id":113,"record_type":"album","type":"album"},{"id":302127,"title":"Discovery","genre_id":106,"record_type":"album","type":"album"},{"id":301775,"title":"Homework","genre_id":113,"record_type":"album","type":"album"},{"id":325477,"title":"Revolution 909","genre_id":113,"record_type":"ep","type":"album"},{"id":325476,"title":"Burnin'","genre_id":113,"record_type":"ep","type":"album"},{"id":302364,"title":"Around the World","genre_id":113,"record_type":"ep","type":"album"},{"id":431082537,"title":"GLBTM (Studio Outtakes)","genre_id":106,"record_type":"single","type":"album"},{"id":418374497,"title":"The Writing of Fragments of Time","genre_id":106,"record_type":"single","type":"album"}
],"total":38,"next":"NEXT"}`

const fixtureArtistAlbumsPage2 = `{"data":[
{"id":7561419,"title":"Derezzed (From \u201cTRON: Legacy\u201d Avicii \"So Amazing Mix\" Feat. Negin)","genre_id":113,"record_type":"single","type":"album"},{"id":6703346,"title":"Get Lucky (feat. Pharrell Williams & Nile Rodgers)","genre_id":113,"record_type":"single","type":"album"},{"id":6516139,"title":"Get Lucky (Radio Edit - feat. Pharrell Williams and Nile Rodgers)","genre_id":106,"record_type":"single","type":"album"},{"id":3492561,"title":"Human After All (Medley)","genre_id":113,"record_type":"single","type":"album"},{"id":7294967,"title":"Harder, Better, Faster, Stronger","genre_id":113,"record_type":"single","type":"album"},{"id":7294838,"title":"Harder, Better, Faster, Stronger (Live)","genre_id":113,"record_type":"single","type":"album"},{"id":325490,"title":"Harder, Better, Faster, Stronger (Alive Radio Edit 2007)","genre_id":132,"record_type":"single","type":"album"},{"id":325424,"title":"Human After All","genre_id":113,"record_type":"single","type":"album"},{"id":303165,"title":"Technologic","genre_id":113,"record_type":"single","type":"album"},{"id":303567,"title":"Technologic (Radio Edit)","genre_id":113,"record_type":"single","type":"album"},{"id":325470,"title":"Robot Rock (Edit)","genre_id":113,"record_type":"single","type":"album"},{"id":299734,"title":"Something About Us (Love Theme from Interstella)","genre_id":113,"record_type":"single","type":"album"},{"id":302378,"title":"Harder, Better, Faster, Stronger","genre_id":113,"record_type":"single","type":"album"},{"id":325478,"title":"Digital Love","genre_id":113,"record_type":"single","type":"album"},{"id":303599,"title":"Aerodynamic","genre_id":113,"record_type":"single","type":"album"},{"id":680155971,"title":"Veridis Quo","genre_id":113,"record_type":"single","type":"album"},{"id":303598,"title":"One More Time","genre_id":113,"record_type":"single","type":"album"},{"id":325475,"title":"Da Funk","genre_id":113,"record_type":"single","type":"album"}
],"total":38}`

// Recorded from https://api.deezer.com/genre with Accept-Language: de, trimmed to id and name.
const fixtureGenreList = `{"data":[{"id":0,"name":"Alle"},{"id":132,"name":"Pop"},{"id":457,"name":"H\u00f6rb\u00fccher"},{"id":116,"name":"Rap\/Hip Hop"},{"id":152,"name":"Rock"},{"id":113,"name":"Dance"},{"id":165,"name":"R&B"},{"id":85,"name":"Alternative"},{"id":106,"name":"Electro"},{"id":466,"name":"Folk"},{"id":144,"name":"Reggae"},{"id":129,"name":"Jazz"},{"id":84,"name":"Country"},{"id":98,"name":"Klassik"},{"id":173,"name":"Filme\/Videospiele"},{"id":464,"name":"Heavy Metal"},{"id":169,"name":"Soul & Funk"},{"id":2,"name":"Afrikanische Musik"},{"id":16,"name":"Asiatische Musik"},{"id":153,"name":"Blues"},{"id":75,"name":"Brasilianische Musik"},{"id":459,"name":"Deutsche Musik"},{"id":81,"name":"Indische Musik"},{"id":95,"name":"Kids"},{"id":197,"name":"Latin Musik"}]}`

func TestArtistGenresFromAlbumFrequency(t *testing.T) {
	var genreListCalls atomic.Int32
	var base string
	provider := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept-Language") != "de" {
			t.Errorf("genre names depend on the caller: Accept-Language %q", r.Header.Get("Accept-Language"))
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/artist/27/albums" && r.URL.Query().Get("index") == "":
			_, _ = w.Write([]byte(strings.Replace(fixtureArtistAlbumsPage1, "NEXT", base+"/artist/27/albums?limit=100&index=20", 1)))
		case r.URL.Path == "/artist/27/albums":
			_, _ = w.Write([]byte(fixtureArtistAlbumsPage2))
		case r.URL.Path == "/genre":
			genreListCalls.Add(1)
			_, _ = w.Write([]byte(fixtureGenreList))
		default:
			t.Errorf("unexpected request %s", r.URL)
			http.NotFound(w, r)
		}
	})
	base = provider.client.baseURL
	for range 2 {
		genres, err := provider.ArtistGenres(t.Context(), "27")
		if err != nil {
			t.Fatal(err)
		}
		// Film/Videospiele (2) and Pop (1) stay below a quarter of Dance (27).
		if !reflect.DeepEqual(genres, []string{"Dance", "Electro"}) {
			t.Fatalf("genres = %v", genres)
		}
	}
	if genreListCalls.Load() != 1 {
		t.Fatalf("genre list read %d times, want once", genreListCalls.Load())
	}
}

func TestDeriveGenreIDs(t *testing.T) {
	for name, tc := range map[string]struct {
		counts map[int64]int
		want   []int64
	}{
		"unknown and all never count": {map[int64]int{genreUnknown: 50, genreAll: 9, 152: 1}, []int64{152}},
		"only unknown":                {map[int64]int{genreUnknown: 3}, []int64{}},
		"no albums":                   {map[int64]int{}, []int64{}},
		"ties by id":                  {map[int64]int{152: 4, 85: 4, 132: 4}, []int64{85, 132, 152}},
		"at most three":               {map[int64]int{1: 5, 2: 5, 3: 5, 4: 5}, []int64{1, 2, 3}},
		"a quarter of the top":        {map[int64]int{165: 49, 173: 11, 116: 10, 132: 13}, []int64{165, 132}},
	} {
		if got := deriveGenreIDs(tc.counts); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: got %v, want %v", name, got, tc.want)
		}
	}
}

func TestArtistGenresResolvesIDsOutsideTheList(t *testing.T) {
	var single atomic.Int32
	provider := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/artist/5/albums":
			// 522 is a real album genre missing from /genre; 9999 is unknown.
			_, _ = w.Write([]byte(`{"data":[{"id":1,"genre_id":522},{"id":2,"genre_id":522},{"id":3,"genre_id":9999},{"id":4,"genre_id":-1}],"total":4}`))
		case "/genre":
			_, _ = w.Write([]byte(fixtureGenreList))
		case "/genre/522":
			single.Add(1)
			_, _ = w.Write([]byte(`{"id":522,"name":"Singer & Songwriter","type":"genre"}`))
		case "/genre/9999":
			_, _ = w.Write([]byte(`{"error":{"type":"DataException","message":"no data","code":800}}`))
		default:
			t.Errorf("unexpected request %s", r.URL)
		}
	})
	for range 2 {
		genres, err := provider.ArtistGenres(t.Context(), "5")
		if err != nil || !reflect.DeepEqual(genres, []string{"Singer & Songwriter"}) {
			t.Fatalf("genres = %v, %v", genres, err)
		}
	}
	if single.Load() != 1 {
		t.Fatalf("genre 522 read %d times, want once", single.Load())
	}
}

func TestAlbumArtist(t *testing.T) {
	provider := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Trimmed from https://api.deezer.com/album/302127.
		_, _ = w.Write([]byte(`{"id":302127,"title":"Discovery","genre_id":106,"artist":{"id":27,"name":"Daft Punk","type":"artist"},"type":"album"}`))
	})
	id, name, err := provider.AlbumArtist(t.Context(), "302127")
	if err != nil || id != "27" || name != "Daft Punk" {
		t.Fatalf("album artist = %q %q %v", id, name, err)
	}
	if _, err := provider.ArtistGenres(t.Context(), "x"); err == nil {
		t.Fatal("invalid id accepted")
	}
}

// A slow genre request must not block lookups the cache can answer.
func TestGenreCacheIsNotLockedDuringRequests(t *testing.T) {
	release := make(chan struct{})
	entered := make(chan struct{}, 1)
	provider := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/genre/522" {
			entered <- struct{}{}
			<-release
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":522,"name":"Singer & Songwriter"}`))
	})
	provider.genres.fill([]apiGenre{{ID: 132, Name: "Pop"}})
	go func() { _, _ = provider.genreName(t.Context(), 522) }()
	<-entered
	done := make(chan string, 1)
	go func() {
		name, _ := provider.genreName(t.Context(), 132)
		done <- name
	}()
	select {
	case name := <-done:
		if name != "Pop" {
			t.Fatalf("name %q", name)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cached genre waited for another genre's request")
	}
	close(release)
}
