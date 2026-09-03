package library

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScannerWalksLibraryAndFilters(t *testing.T) {
	root := t.TempDir()

	// Create valid directory hierarchy
	albumDir := filepath.Join(root, "Artist", "Album")
	if err := os.MkdirAll(albumDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Valid audio files
	track1 := filepath.Join(albumDir, "01 - Track.opus")
	track2 := filepath.Join(albumDir, "02 - Track.flac")
	if err := os.WriteFile(track1, []byte("opus audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(track2, []byte("flac audio"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Files to ignore
	if err := os.WriteFile(filepath.Join(albumDir, "cover.jpg"), []byte("cover image"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(albumDir, ".ytdm-temp.opus"), []byte("temp"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(albumDir, ".DS_Store"), []byte("system"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(albumDir, "notes.txt"), []byte("text"), 0o644); err != nil {
		t.Fatal(err)
	}

	discovered, err := WalkLibraryFiles(root)
	if err != nil {
		t.Fatalf("WalkLibraryFiles failed: %v", err)
	}

	if len(discovered) != 2 {
		t.Fatalf("expected 2 audio files, got %d: %+v", len(discovered), discovered)
	}

	rel1 := filepath.Join("Artist", "Album", "01 - Track.opus")
	rel2 := filepath.Join("Artist", "Album", "02 - Track.flac")
	paths := map[string]bool{}
	for _, d := range discovered {
		paths[d.RelPath] = true
	}

	if !paths[rel1] || !paths[rel2] {
		t.Fatalf("expected files %q and %q, got: %+v", rel1, rel2, paths)
	}
}

func TestScannerSymlinkEscapePrevention(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()

	secretFile := filepath.Join(outside, "secret.opus")
	if err := os.WriteFile(secretFile, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create symlink inside root pointing outside
	linkInside := filepath.Join(root, "escaped.opus")
	if err := os.Symlink(secretFile, linkInside); err != nil {
		t.Skipf("symlink not supported on this platform: %v", err)
	}

	discovered, err := WalkLibraryFiles(root)
	if err != nil {
		t.Fatalf("WalkLibraryFiles: %v", err)
	}

	for _, d := range discovered {
		if d.RelPath == "escaped.opus" {
			t.Fatal("scanner must ignore symlinks escaping library root")
		}
	}
}

func TestVerifyPathConfinement(t *testing.T) {
	root := t.TempDir()

	artistDir := filepath.Join(root, "Artist", "2020 - Album")
	if err := os.MkdirAll(artistDir, 0o755); err != nil {
		t.Fatal(err)
	}

	existingFile := filepath.Join(artistDir, "01.opus")
	if err := os.WriteFile(existingFile, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}

	// 1. Existing file inside root -> OK
	abs, rel, err := VerifyPathConfinement(root, existingFile, false)
	if err != nil {
		t.Fatalf("expected existing file to be valid: %v", err)
	}
	if rel != filepath.Join("Artist", "2020 - Album", "01.opus") {
		t.Fatalf("unexpected rel path: %s", rel)
	}
	evalExisting, _ := filepath.EvalSymlinks(existingFile)
	if abs != evalExisting {
		t.Fatalf("unexpected abs path: %s, want %s", abs, evalExisting)
	}

	// 2. Missing file inside existing directory (allowMissing = true) -> OK
	missingFile := filepath.Join(artistDir, "02.opus")
	absMissing, relMissing, err := VerifyPathConfinement(root, missingFile, true)
	if err != nil {
		t.Fatalf("expected missing file with allowMissing=true to be valid: %v", err)
	}
	if relMissing != filepath.Join("Artist", "2020 - Album", "02.opus") {
		t.Fatalf("unexpected rel missing path: %s", relMissing)
	}
	evalArtistDir, _ := filepath.EvalSymlinks(artistDir)
	if absMissing != filepath.Join(evalArtistDir, "02.opus") {
		t.Fatalf("unexpected abs missing path: %s", absMissing)
	}

	// 3. Missing file inside non-existing subdirectory inside root (allowMissing = true) -> OK
	nestedMissing := filepath.Join(root, "NewArtist", "NewAlbum", "01.opus")
	absNested, relNested, err := VerifyPathConfinement(root, nestedMissing, true)
	if err != nil {
		t.Fatalf("expected nested missing file with allowMissing=true to be valid: %v", err)
	}
	if relNested != filepath.Join("NewArtist", "NewAlbum", "01.opus") {
		t.Fatalf("unexpected rel nested missing path: %s", relNested)
	}
	_ = absNested

	// 4. Missing file with allowMissing = false -> Error
	_, _, err = VerifyPathConfinement(root, missingFile, false)
	if err == nil {
		t.Fatal("expected error for missing file when allowMissing=false")
	}

	// 5. Path Traversal -> Error
	traversalPath := filepath.Join(root, "..", "outside.opus")
	_, _, err = VerifyPathConfinement(root, traversalPath, true)
	if err == nil {
		t.Fatal("expected error for path traversal")
	}

	// 6. Absolute path outside root -> Error
	outsideFile := filepath.Join(os.TempDir(), "random.opus")
	_, _, err = VerifyPathConfinement(root, outsideFile, true)
	if err == nil {
		t.Fatal("expected error for outside path")
	}
}

func TestScanner_ReservedAndDotDirectories(t *testing.T) {
	root := t.TempDir()

	// 1. Reserved internal directories should be ignored
	reservedTrash := filepath.Join(root, ".ytmdl-trash", "orphan.opus")
	_ = os.MkdirAll(filepath.Dir(reservedTrash), 0o755)
	_ = os.WriteFile(reservedTrash, []byte("trash"), 0o644)

	reservedFinalize := filepath.Join(root, ".ytmdl-finalize", "temp.opus")
	_ = os.MkdirAll(filepath.Dir(reservedFinalize), 0o755)
	_ = os.WriteFile(reservedFinalize, []byte("finalize"), 0o644)

	reservedGit := filepath.Join(root, ".git", "hook.opus")
	_ = os.MkdirAll(filepath.Dir(reservedGit), 0o755)
	_ = os.WriteFile(reservedGit, []byte("git"), 0o644)

	// 2. Legitimate dot-prefixed artist directory (e.g. .38 Special) SHOULD be scanned
	legitArtistDir := filepath.Join(root, ".38 Special", "Wild-Eyed Southern Boys")
	_ = os.MkdirAll(legitArtistDir, 0o755)
	legitTrack := filepath.Join(legitArtistDir, "01 - Hold On Loosely.opus")
	_ = os.WriteFile(legitTrack, []byte("audio"), 0o644)

	discovered, err := WalkLibraryFiles(root)
	if err != nil {
		t.Fatalf("WalkLibraryFiles failed: %v", err)
	}

	if len(discovered) != 1 {
		t.Fatalf("expected exactly 1 discovered file, got %d: %+v", len(discovered), discovered)
	}

	expectedRel := filepath.Join(".38 Special", "Wild-Eyed Southern Boys", "01 - Hold On Loosely.opus")
	if discovered[0].RelPath != expectedRel {
		t.Fatalf("expected rel path %q, got %q", expectedRel, discovered[0].RelPath)
	}
}
