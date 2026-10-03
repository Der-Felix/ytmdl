package fingerprint

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"time"
)

type Measurement struct {
	Duration float64  `json:"duration"`
	Words    []uint32 `json:"fingerprint"`
}

// Measure decodes only an already-open local file, with no network transports.
// fpcalc receives bounded raw PCM rather than a filename or provider URL.
func Measure(parent context.Context, f *os.File, ffmpeg, fpcalc string) (Measurement, error) {
	ctx, cancel := context.WithTimeout(parent, 60*time.Second)
	defer cancel()
	decoder := exec.CommandContext(ctx, ffmpeg, "-nostdin", "-v", "error", "-threads", "1", "-protocol_whitelist", "file,pipe", "-format_whitelist", "mp3,flac,ogg,wav,mov,matroska,webm,aac", "-i", "/dev/fd/3", "-t", "90", "-map", "0:a:0", "-vn", "-ar", "11025", "-ac", "1", "-f", "s16le", "pipe:1")
	decoder.ExtraFiles = []*os.File{f}
	reader, writer, pipeErr := os.Pipe()
	if pipeErr != nil {
		return Measurement{}, pipeErr
	}
	defer reader.Close()
	defer writer.Close()
	decoder.Stdout = writer
	calculator := exec.CommandContext(ctx, fpcalc, "-format", "s16le", "-rate", "11025", "-channels", "1", "-length", "90", "-algorithm", "2", "-raw", "-json", "-")
	calculator.Stdin = reader
	output, err := calculator.StdoutPipe()
	if err != nil {
		return Measurement{}, err
	}
	if err = calculator.Start(); err != nil {
		return Measurement{}, err
	}
	if err = decoder.Start(); err != nil {
		cancel()
		reader.Close()
		_ = calculator.Wait()
		return Measurement{}, err
	}
	// Child processes own their pipe descriptors. Close the parent copies so
	// early decoder/calculator failures cannot leave a goroutine waiting on EOF.
	reader.Close()
	writer.Close()
	raw, readErr := io.ReadAll(io.LimitReader(output, 65537))
	if readErr != nil || len(raw) > 65536 {
		cancel()
	}
	calcErr := calculator.Wait()
	decodeErr := decoder.Wait()
	var m Measurement
	// fpcalc reports exit 3 on the expected EOF of a bounded raw stream; accept
	// only a successful decoder, finite bounded JSON and a complete measurement.
	var exit *exec.ExitError
	if readErr != nil || decodeErr != nil || len(raw) > 65536 || ctx.Err() != nil || calcErr != nil && (!errors.As(calcErr, &exit) || exit.ExitCode() != 3) || json.Unmarshal(raw, &m) != nil || m.Duration < 0 || m.Duration > 91 || len(m.Words) > 1200 {
		return Measurement{}, errors.New("audio analysis unavailable")
	}
	return m, nil
}
