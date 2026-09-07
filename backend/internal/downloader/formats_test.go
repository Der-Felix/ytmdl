package downloader_test

import (
	"testing"

	"ytdm/backend/internal/downloader"
	"ytdm/backend/internal/provider"
)

func TestSelectFormat_SoundCloud(t *testing.T) {
	// Representative SoundCloud format list
	formats := []provider.AudioFormat{
		{
			ID:          "hls_mp3_128k",
			Codec:       "mp3",
			Container:   "mp3",
			BitrateKbps: 128,
		},
		{
			ID:          "hls_aac_160k",
			Codec:       "aac",
			Container:   "m4a",
			BitrateKbps: 160,
		},
	}

	selected, ok := downloader.SelectFormat(formats)
	if !ok {
		t.Fatal("expected format to be selected")
	}
	// Higher bitrate wins when neither is Opus
	if selected.ID != "hls_aac_160k" {
		t.Errorf("expected hls_aac_160k to be selected, got %s", selected.ID)
	}

	// When Opus stream is present on SoundCloud
	formatsWithOpus := append(formats, provider.AudioFormat{
		ID:          "hls_opus_64k",
		Codec:       "opus",
		Container:   "ogg",
		BitrateKbps: 64,
	})
	selectedOpus, ok := downloader.SelectFormat(formatsWithOpus)
	if !ok {
		t.Fatal("expected format to be selected")
	}
	// Opus always wins regardless of bitrate
	if selectedOpus.ID != "hls_opus_64k" {
		t.Errorf("expected hls_opus_64k to be selected, got %s", selectedOpus.ID)
	}
}

func TestPlanFor_SoundCloudFormats(t *testing.T) {
	tests := []struct {
		name           string
		info           downloader.AudioInfo
		allowTranscode bool
		wantPlan       downloader.Plan
		wantExt        string
	}{
		{
			name: "Native SoundCloud MP3 without transcode",
			info: downloader.AudioInfo{
				Codec:     "mp3",
				Container: "mp3",
			},
			allowTranscode: false,
			wantPlan:       downloader.PlanKeep,
			wantExt:        ".mp3",
		},
		{
			name: "Native SoundCloud AAC/M4A without transcode",
			info: downloader.AudioInfo{
				Codec:     "aac",
				Container: "m4a",
			},
			allowTranscode: false,
			wantPlan:       downloader.PlanKeep,
			wantExt:        ".m4a",
		},
		{
			name: "Native SoundCloud Opus in Ogg",
			info: downloader.AudioInfo{
				Codec:     "opus",
				Container: "ogg",
			},
			allowTranscode: false,
			wantPlan:       downloader.PlanKeep,
			wantExt:        ".opus",
		},
		{
			name: "SoundCloud MP3 with allowTranscode",
			info: downloader.AudioInfo{
				Codec:     "mp3",
				Container: "mp3",
			},
			allowTranscode: true,
			wantPlan:       downloader.PlanTranscode,
			wantExt:        ".opus",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan, ext := downloader.PlanFor(tt.info, tt.allowTranscode)
			if plan != tt.wantPlan {
				t.Errorf("PlanFor() plan = %v, want %v", plan, tt.wantPlan)
			}
			if ext != tt.wantExt {
				t.Errorf("PlanFor() ext = %v, want %v", ext, tt.wantExt)
			}
		})
	}
}
