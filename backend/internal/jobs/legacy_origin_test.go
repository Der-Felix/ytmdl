package jobs

import (
	"context"
	"encoding/json"
	"testing"

	"ytdm/backend/internal/orchestrator"
)

// FINDING 4: newly created jobs are explicitly tagged, and legacy jobs written
// before v0.26 (options.origin == "") are classified from durable job data.
//
// Subscription automation has only ever queued release jobs, so an untagged
// artist or track job is provably manual, while an untagged release job is
// ambiguous and must stay on the conservative subscription policy.
func TestFinding4_ResolveOrigin_Classification(t *testing.T) {
	cases := []struct {
		name string
		job  Job
		want Origin
	}{
		{
			name: "new manual artist job",
			job:  Job{Type: TypeArtist, Options: Options{Origin: OriginManual}},
			want: OriginManual,
		},
		{
			name: "new manual release job",
			job:  Job{Type: TypeRelease, Options: Options{Origin: OriginManual}},
			want: OriginManual,
		},
		{
			name: "new manual track job",
			job:  Job{Type: TypeTrack, Options: Options{Origin: OriginManual}},
			want: OriginManual,
		},
		{
			name: "new subscription release job",
			job:  Job{Type: TypeRelease, Options: Options{Origin: OriginSubscription}},
			want: OriginSubscription,
		},
		{
			name: "legacy provable-manual artist job",
			job:  Job{Type: TypeArtist},
			want: OriginManual,
		},
		{
			name: "legacy provable-manual track job",
			job:  Job{Type: TypeTrack},
			want: OriginManual,
		},
		{
			name: "legacy ambiguous release job stays conservative",
			job:  Job{Type: TypeRelease},
			want: OriginSubscription,
		},
		{
			name: "unknown origin value on a release job stays conservative",
			job:  Job{Type: TypeRelease, Options: Options{Origin: Origin("bogus")}},
			want: OriginSubscription,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			job := tc.job
			if got := job.ResolveOrigin(); got != tc.want {
				t.Fatalf("ResolveOrigin() = %q, want %q", got, tc.want)
			}
		})
	}
}

// FINDING 4: no legacy job whose origin cannot be proven manual may be
// classified as manual, since that would let a historical background backlog
// gain automatic SoundCloud substitution after an upgrade.
func TestFinding4_AmbiguousLegacyJob_NeverBecomesManual(t *testing.T) {
	// Every job the subscription scheduler has ever created is a release job.
	subscriptionShapes := []Job{
		{Type: TypeRelease},
		{Type: TypeRelease, Label: "Some Artist"},
		{Type: TypeRelease, Priority: PriorityLow, Options: Options{SkipExisting: true}},
		{Type: TypeRelease, Options: Options{Origin: Origin("")}},
	}
	for _, job := range subscriptionShapes {
		j := job
		if got := j.ResolveOrigin(); got != OriginSubscription {
			t.Fatalf("legacy release job %+v resolved to %q; ambiguous legacy work must never become manual", j, got)
		}
	}
}

// FINDING 4: classification is a pure read of durable fields, so retries,
// restarts, and reloads from the store all produce the same origin.
func TestFinding4_ResolveOrigin_StableAcrossPersistenceAndRetries(t *testing.T) {
	jobs := []Job{
		{Type: TypeArtist, Options: Options{Origin: OriginManual}},
		{Type: TypeRelease, Options: Options{Origin: OriginSubscription}},
		{Type: TypeArtist},
		{Type: TypeRelease},
		{Type: TypeTrack},
	}

	for _, job := range jobs {
		j := job
		want := j.ResolveOrigin()

		// Round-trip the persisted options_json payload.
		raw, err := json.Marshal(j.Options)
		if err != nil {
			t.Fatalf("marshal options: %v", err)
		}
		reloaded := Job{Type: j.Type}
		if err := json.Unmarshal(raw, &reloaded.Options); err != nil {
			t.Fatalf("unmarshal options: %v", err)
		}

		for attempt := 1; attempt <= 3; attempt++ {
			if got := reloaded.ResolveOrigin(); got != want {
				t.Fatalf("attempt %d: reloaded origin = %q, want %q", attempt, got, want)
			}
		}
	}
}

// FINDING 4: a nil job must not be treated as manual.
func TestFinding4_NilJob_ResolvesConservatively(t *testing.T) {
	var job *Job
	if got := job.ResolveOrigin(); got != OriginSubscription {
		t.Fatalf("ResolveOrigin() = %q, want %q", got, OriginSubscription)
	}
}

// FINDING 4: every new job created through the manager carries an explicit origin.
func TestFinding4_NewJobs_AreExplicitlyTagged(t *testing.T) {
	store := &atomicEnqueueStore{}
	m := newTestManagerForPriority(store)

	manualOrigin := OriginManual
	manual, err := m.Enqueue(context.Background(), Request{
		Type:             TypeRelease,
		MetadataProvider: "ytmusic",
		TargetID:         "release-manual",
		Options:          RequestOptions{Origin: &manualOrigin},
	})
	if err != nil {
		t.Fatalf("manual Enqueue: %v", err)
	}
	if manual.Options.Origin != OriginManual {
		t.Fatalf("manual job origin = %q, want manual", manual.Options.Origin)
	}
	if manual.ResolveOrigin() != OriginManual {
		t.Fatalf("manual job ResolveOrigin = %q, want manual", manual.ResolveOrigin())
	}

	queued, err := m.EnqueueReleaseWithPriority(context.Background(), "ytmusic", "release-sub", "Some Artist", PriorityLow)
	if err != nil {
		t.Fatalf("subscription Enqueue: %v", err)
	}
	if !queued {
		t.Fatal("expected the subscription release job to be queued")
	}

	store.mu.Lock()
	defer store.mu.Unlock()
	var found bool
	for _, job := range store.jobs {
		if job.TargetID != "release-sub" {
			continue
		}
		found = true
		if job.Options.Origin != OriginSubscription {
			t.Fatalf("subscription job origin = %q, want subscription", job.Options.Origin)
		}
		j := job
		if j.ResolveOrigin() != OriginSubscription {
			t.Fatalf("subscription job ResolveOrigin = %q, want subscription", j.ResolveOrigin())
		}
	}
	if !found {
		t.Fatal("subscription job was not persisted")
	}
}

// FINDING 4 / CROSS-FINDING H: the origin the worker hands to the orchestrator
// never gives legacy background work the aggressive manual policy.
func TestFinding4_ResolutionOrigin_MappingUsedByWorker(t *testing.T) {
	cases := []struct {
		name string
		job  Job
		want orchestrator.Origin
	}{
		{"tagged manual", Job{Type: TypeRelease, Options: Options{Origin: OriginManual}}, orchestrator.OriginManual},
		{"tagged subscription", Job{Type: TypeRelease, Options: Options{Origin: OriginSubscription}}, orchestrator.OriginSubscription},
		{"legacy artist", Job{Type: TypeArtist}, orchestrator.OriginManual},
		{"legacy track", Job{Type: TypeTrack}, orchestrator.OriginManual},
		{"legacy release", Job{Type: TypeRelease}, orchestrator.OriginSubscription},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolutionOrigin(tc.job); got != tc.want {
				t.Fatalf("resolutionOrigin() = %q, want %q", got, tc.want)
			}
		})
	}
}
