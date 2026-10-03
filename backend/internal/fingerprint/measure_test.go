package fingerprint

import (
	"context"
	"encoding/binary"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestRealChromaprintAcrossCodecsAndDifferentMusic(t *testing.T) {
	ff, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not available")
	}
	calc, err := exec.LookPath("fpcalc")
	if err != nil {
		t.Skip("Chromaprint not available")
	}
	dir := t.TempDir()
	write := func(name string, variation int) string {
		rate, n := 22050, 22050*40
		raw := make([]byte, 44+n*2)
		copy(raw, "RIFF")
		binary.LittleEndian.PutUint32(raw[4:], uint32(len(raw)-8))
		copy(raw[8:], "WAVEfmt ")
		binary.LittleEndian.PutUint32(raw[16:], 16)
		binary.LittleEndian.PutUint16(raw[20:], 1)
		binary.LittleEndian.PutUint16(raw[22:], 1)
		binary.LittleEndian.PutUint32(raw[24:], uint32(rate))
		binary.LittleEndian.PutUint32(raw[28:], uint32(rate*2))
		binary.LittleEndian.PutUint16(raw[32:], 2)
		binary.LittleEndian.PutUint16(raw[34:], 16)
		copy(raw[36:], "data")
		binary.LittleEndian.PutUint32(raw[40:], uint32(n*2))
		chords := [][]float64{{220, 277.18, 329.63}, {196, 246.94, 293.66}, {174.61, 220, 261.63}, {164.81, 207.65, 246.94}}
		for i := 0; i < n; i++ {
			tm := float64(i) / float64(rate)
			step := int(tm / 0.75)
			ch := chords[(step*(1+variation)+variation)%4]
			v := 0.0
			for j, f := range ch {
				phase := tm * f * (1 + float64(variation)*0.06)
				v += math.Sin(2*math.Pi*phase)*0.14 + math.Sin(4*math.Pi*phase)*0.025
				v += math.Sin(2*math.Pi*(f*2+float64(j)*5)*tm) * 0.02
			}
			v *= 0.35 + 0.65*math.Exp(-math.Mod(tm, 0.25)*10)
			binary.LittleEndian.PutUint16(raw[44+i*2:], uint16(int16(v*30000)))
		}
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, raw, 0600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	source := write("source.wav", 0)
	other := write("different.wav", 1)
	measure := func(path string) Measurement {
		f, e := os.Open(path)
		if e != nil {
			t.Fatal(e)
		}
		defer f.Close()
		m, e := Measure(context.Background(), f, ff, calc)
		if e != nil {
			t.Fatal("bounded local measurement failed", e)
		}
		return m
	}
	original := measure(source)
	if !Usable(original.Words) {
		t.Fatal("fixture measurement is not informative")
	}
	for _, format := range []string{"opus", "mp3"} {
		dest := filepath.Join(dir, "converted."+format)
		cmd := exec.Command(ff, "-v", "error", "-i", source, "-b:a", "128k", dest)
		if e := cmd.Run(); e != nil {
			t.Fatal("fixture encoding failed")
		}
		m := measure(dest)
		score := Similarity(original.Words, m.Words)
		t.Logf("%s similarity %.3f", format, score)
		if score < 0.92 {
			t.Fatal("same music across codecs was missed")
		}
	}
	different := measure(other)
	score := Similarity(original.Words, different.Words)
	t.Logf("different music similarity %.3f", score)
	if score >= 0.92 {
		t.Fatal("different music matched")
	}

	malformed := filepath.Join(dir, "malformed.opus")
	if err := os.WriteFile(malformed, []byte("invalid fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	bad, _ := os.Open(malformed)
	defer bad.Close()
	if _, err := Measure(context.Background(), bad, ff, calc); err == nil {
		t.Fatal("malformed audio was accepted")
	}
	f, _ := os.Open(source)
	defer f.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Measure(ctx, f, ff, calc); err == nil {
		t.Fatal("canceled analysis succeeded")
	}
}
