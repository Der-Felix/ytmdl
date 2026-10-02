package handlers

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func TestArtworkRasterValidation(t *testing.T) {
	for _, data := range [][]byte{[]byte("<svg xmlns='http://www.w3.org/2000/svg'><script>alert(1)</script></svg>"), []byte("not an image")} {
		if _, err := normalizeArtwork(data); err == nil {
			t.Fatal("unsafe artwork accepted")
		}
	}
	img := image.NewRGBA(image.Rect(0, 0, 20, 10))
	img.Set(0, 0, color.RGBA{255, 0, 0, 255})
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	data, err := normalizeArtwork(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || format != "jpeg" || cfg.Width != 20 || cfg.Height != 10 {
		t.Fatal("upload not normalized")
	}
}
func TestLoudnessBoundariesAndPeakProtection(t *testing.T) {
	for _, tc := range []struct {
		data string
		gain float64
	}{{`analysis prefix
{"input_i":"-10","input_tp":"-0.5"}
final progress summary`, -6}, {`{"input_i":"-30","input_tp":"-1"}`, -0.5}, {`{"input_i":"-30","input_tp":"-20"}`, 6}, {`{"input_i":"-inf","input_tp":"-inf"}`, 0}} {
		l, err := parseLoudness([]byte(tc.data))
		if err != nil || l.GainDB != tc.gain {
			t.Fatalf("%s: %+v %v", tc.data, l, err)
		}
	}
	for _, data := range []string{`{"input_i":"NaN","input_tp":"-2"}`, `{"input_i":"-20"}`, `broken`} {
		if _, err := parseLoudness([]byte(data)); err == nil {
			t.Fatal("invalid loudness accepted")
		}
	}
}
