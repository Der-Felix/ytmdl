package metadata

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"ytdm/backend/internal/apperr"
)

// This file implements just enough of RFC 3533 (Ogg) and RFC 7845 (Ogg Opus)
// to replace the comment header of an existing file. Rewriting the header
// leaves every audio page byte identical, which is what keeps the stored Opus
// stream the platform's own — no decode, no re-encode.

const (
	oggCapturePattern  = "OggS"
	oggHeaderSize      = 27
	maxSegmentsPerPage = 255
	maxSegmentSize     = 255

	headerTypeContinued = 0x01
	headerTypeBOS       = 0x02

	opusTagsMagic = "OpusTags"
	opusHeadMagic = "OpusHead"
)

// oggCRCTable is the CRC-32 table for the polynomial Ogg uses (0x04c11db7,
// no reflection, no final inversion).
var oggCRCTable = func() [256]uint32 {
	var table [256]uint32
	for i := range table {
		r := uint32(i) << 24
		for range 8 {
			if r&0x80000000 != 0 {
				r = (r << 1) ^ 0x04c11db7
			} else {
				r <<= 1
			}
		}
		table[i] = r
	}
	return table
}()

func oggCRC(data []byte) uint32 {
	var crc uint32
	for _, b := range data {
		crc = (crc << 8) ^ oggCRCTable[byte(crc>>24)^b]
	}
	return crc
}

// oggPage is a decoded Ogg page.
type oggPage struct {
	headerType byte
	granule    int64
	serial     uint32
	sequence   uint32
	segments   []byte
	payload    []byte
}

// endsPacket reports whether the last packet in this page is complete.
func (p *oggPage) endsPacket() bool {
	return len(p.segments) > 0 && p.segments[len(p.segments)-1] < maxSegmentSize
}

// encode renders the page with the given sequence number and a fresh checksum.
func (p *oggPage) encode(sequence uint32) []byte {
	buf := make([]byte, 0, oggHeaderSize+len(p.segments)+len(p.payload))
	buf = append(buf, oggCapturePattern...)
	buf = append(buf, 0) // stream structure version
	buf = append(buf, p.headerType)
	buf = binary.LittleEndian.AppendUint64(buf, uint64(p.granule))
	buf = binary.LittleEndian.AppendUint32(buf, p.serial)
	buf = binary.LittleEndian.AppendUint32(buf, sequence)
	buf = binary.LittleEndian.AppendUint32(buf, 0) // checksum placeholder
	buf = append(buf, byte(len(p.segments)))
	buf = append(buf, p.segments...)
	buf = append(buf, p.payload...)

	crc := oggCRC(buf)
	binary.LittleEndian.PutUint32(buf[22:26], crc)
	return buf
}

// readPage reads the next Ogg page.
func readPage(r *bufio.Reader) (*oggPage, error) {
	header := make([]byte, oggHeaderSize)
	if _, err := io.ReadFull(r, header); err != nil {
		return nil, err
	}
	if string(header[0:4]) != oggCapturePattern {
		return nil, apperr.New(apperr.CodeInvalidAudio, "The file is not a valid Ogg stream.")
	}
	if header[4] != 0 {
		return nil, apperr.Newf(apperr.CodeInvalidAudio, "Unsupported Ogg version %d.", header[4])
	}

	page := &oggPage{
		headerType: header[5],
		granule:    int64(binary.LittleEndian.Uint64(header[6:14])),
		serial:     binary.LittleEndian.Uint32(header[14:18]),
		sequence:   binary.LittleEndian.Uint32(header[18:22]),
	}

	count := int(header[26])
	page.segments = make([]byte, count)
	if _, err := io.ReadFull(r, page.segments); err != nil {
		return nil, apperr.Wrap(apperr.CodeInvalidAudio, "The Ogg segment table is truncated.", err)
	}

	size := 0
	for _, s := range page.segments {
		size += int(s)
	}
	page.payload = make([]byte, size)
	if _, err := io.ReadFull(r, page.payload); err != nil {
		return nil, apperr.Wrap(apperr.CodeInvalidAudio, "An Ogg page is truncated.", err)
	}
	return page, nil
}

// lacing splits a packet length into Ogg lacing values.
func lacing(length int) []byte {
	segments := make([]byte, 0, length/maxSegmentSize+1)
	for length >= maxSegmentSize {
		segments = append(segments, maxSegmentSize)
		length -= maxSegmentSize
	}
	// A trailing value below 255 terminates the packet. A packet whose length
	// is an exact multiple of 255 therefore needs an explicit zero segment.
	segments = append(segments, byte(length))
	return segments
}

// writePacketPages writes a single packet as one or more pages and returns the
// next free sequence number.
func writePacketPages(w io.Writer, serial, sequence uint32, granule int64, packet []byte) (uint32, error) {
	segments := lacing(len(packet))

	offset := 0
	for index := 0; index < len(segments); index += maxSegmentsPerPage {
		end := min(index+maxSegmentsPerPage, len(segments))
		group := segments[index:end]

		size := 0
		for _, s := range group {
			size += int(s)
		}

		page := &oggPage{
			granule:  granule,
			serial:   serial,
			segments: group,
			payload:  packet[offset : offset+size],
		}
		if index > 0 {
			page.headerType = headerTypeContinued
		}
		if _, err := w.Write(page.encode(sequence)); err != nil {
			return 0, apperr.Wrap(apperr.CodeTaggingFailed, "The Ogg page could not be written.", err)
		}
		offset += size
		sequence++
	}
	return sequence, nil
}

// buildOpusTags renders an OpusTags packet.
func buildOpusTags(vendor string, comments []string) []byte {
	buf := bytes.NewBuffer(make([]byte, 0, 512))
	buf.WriteString(opusTagsMagic)

	vendorBytes := []byte(vendor)
	_ = binary.Write(buf, binary.LittleEndian, uint32(len(vendorBytes)))
	buf.Write(vendorBytes)

	_ = binary.Write(buf, binary.LittleEndian, uint32(len(comments)))
	for _, comment := range comments {
		raw := []byte(comment)
		_ = binary.Write(buf, binary.LittleEndian, uint32(len(raw)))
		buf.Write(raw)
	}
	return buf.Bytes()
}

// parseVendor reads the vendor string of an existing OpusTags packet.
func parseVendor(packet []byte) (string, error) {
	if len(packet) < len(opusTagsMagic)+4 || string(packet[:len(opusTagsMagic)]) != opusTagsMagic {
		return "", apperr.New(apperr.CodeInvalidAudio, "The Opus comment header is missing.")
	}
	offset := len(opusTagsMagic)
	length := int(binary.LittleEndian.Uint32(packet[offset : offset+4]))
	offset += 4
	if length < 0 || offset+length > len(packet) {
		return "", apperr.New(apperr.CodeInvalidAudio, "The Opus comment header is malformed.")
	}
	return string(packet[offset : offset+length]), nil
}

// writeOpusComments rewrites the comment header of an Ogg Opus file. Audio
// pages are copied byte for byte; only their page sequence numbers and
// checksums are adjusted, because the new comment header may occupy a
// different number of pages than the old one.
func writeOpusComments(path string, comments []string) error {
	input, err := os.Open(path)
	if err != nil {
		return apperr.Wrap(apperr.CodeTaggingFailed, "The audio file could not be opened.", err)
	}
	defer input.Close()

	temp, err := os.CreateTemp(filepath.Dir(path), ".ytdm-tag-*")
	if err != nil {
		return apperr.Wrap(apperr.CodeTaggingFailed, "The temporary file could not be created.", err)
	}
	tempPath := temp.Name()
	defer func() {
		temp.Close()
		os.Remove(tempPath)
	}()

	reader := bufio.NewReaderSize(input, 64*1024)
	writer := bufio.NewWriterSize(temp, 64*1024)

	// Page one carries the identification header and stays as it is.
	head, err := readPage(reader)
	if err != nil {
		return wrapOggRead(err)
	}
	if len(head.payload) < len(opusHeadMagic) || string(head.payload[:len(opusHeadMagic)]) != opusHeadMagic {
		return apperr.New(apperr.CodeInvalidAudio, "The file is not an Ogg Opus stream.")
	}
	if head.headerType&headerTypeBOS == 0 {
		return apperr.New(apperr.CodeInvalidAudio, "The Ogg stream does not start with a begin-of-stream page.")
	}
	if _, err := writer.Write(head.encode(0)); err != nil {
		return apperr.Wrap(apperr.CodeTaggingFailed, "The Opus header could not be written.", err)
	}

	// The comment header follows and may span several pages.
	var tags []byte
	for {
		page, err := readPage(reader)
		if err != nil {
			return wrapOggRead(err)
		}
		if page.serial != head.serial {
			return apperr.New(apperr.CodeInvalidAudio, "Multiplexed Ogg streams are not supported.")
		}
		tags = append(tags, page.payload...)
		if page.endsPacket() {
			break
		}
	}

	vendor, err := parseVendor(tags)
	if err != nil {
		return err
	}

	sequence, err := writePacketPages(writer, head.serial, 1, 0, buildOpusTags(vendor, comments))
	if err != nil {
		return err
	}

	// Everything after the comment header is audio and is copied unchanged
	// apart from its page sequence number.
	for {
		page, err := readPage(reader)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return wrapOggRead(err)
		}
		if _, err := writer.Write(page.encode(sequence)); err != nil {
			return apperr.Wrap(apperr.CodeTaggingFailed, "An audio page could not be written.", err)
		}
		sequence++
	}

	if err := writer.Flush(); err != nil {
		return apperr.Wrap(apperr.CodeTaggingFailed, "The tagged file could not be flushed.", err)
	}
	if err := temp.Sync(); err != nil {
		return apperr.Wrap(apperr.CodeTaggingFailed, "The tagged file could not be synced.", err)
	}
	if err := temp.Close(); err != nil {
		return apperr.Wrap(apperr.CodeTaggingFailed, "The tagged file could not be closed.", err)
	}
	if err := input.Close(); err != nil {
		return apperr.Wrap(apperr.CodeTaggingFailed, "The source file could not be closed.", err)
	}

	info, statErr := os.Stat(path)
	if statErr == nil {
		_ = os.Chmod(tempPath, info.Mode().Perm())
	}
	if err := os.Rename(tempPath, path); err != nil {
		return apperr.Wrap(apperr.CodeTaggingFailed, "The tagged file could not replace the original.", err)
	}
	return nil
}

// wrapOggRead turns an unexpected end of file into a clear application error.
func wrapOggRead(err error) error {
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return apperr.Wrap(apperr.CodeInvalidAudio, "The Ogg stream ended unexpectedly.", err)
	}
	var appErr *apperr.Error
	if errors.As(err, &appErr) {
		return err
	}
	return apperr.Wrap(apperr.CodeInvalidAudio, "The Ogg stream could not be read.", err)
}

// ReadOpusCommentsList reads the raw comment list of an Ogg Opus file.
func ReadOpusCommentsList(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeInvalidAudio, "The audio file could not be opened.", err)
	}
	defer file.Close()

	reader := bufio.NewReaderSize(file, 64*1024)
	if _, err := readPage(reader); err != nil {
		return nil, wrapOggRead(err)
	}

	var packet []byte
	for {
		page, err := readPage(reader)
		if err != nil {
			return nil, wrapOggRead(err)
		}
		packet = append(packet, page.payload...)
		if page.endsPacket() {
			break
		}
	}

	vendor, err := parseVendor(packet)
	if err != nil {
		return nil, err
	}

	offset := len(opusTagsMagic) + 4 + len(vendor)
	if offset+4 > len(packet) {
		return nil, apperr.New(apperr.CodeInvalidAudio, "The Opus comment header is truncated.")
	}
	count := int(binary.LittleEndian.Uint32(packet[offset : offset+4]))
	offset += 4

	out := make([]string, 0, count)
	for range count {
		if offset+4 > len(packet) {
			return nil, apperr.New(apperr.CodeInvalidAudio, "The Opus comment list is truncated.")
		}
		length := int(binary.LittleEndian.Uint32(packet[offset : offset+4]))
		offset += 4
		if length < 0 || offset+length > len(packet) {
			return nil, apperr.New(apperr.CodeInvalidAudio, "An Opus comment is truncated.")
		}
		comment := string(packet[offset : offset+length])
		offset += length
		out = append(out, comment)
	}
	return out, nil
}

// readOpusComments reads the comment list of an Ogg Opus file. It exists so
// that the written tags can be verified.
func readOpusComments(path string) (map[string][]string, error) {
	list, err := ReadOpusCommentsList(path)
	if err != nil {
		return nil, err
	}
	out := make(map[string][]string, len(list))
	for _, comment := range list {
		key, value, found := cutComment(comment)
		if !found {
			continue
		}
		out[key] = append(out[key], value)
	}
	return out, nil
}

// UpdateOpusArtwork replaces or removes the embedded cover art in an Ogg Opus file,
// preserving all audio frames and existing tags completely intact.
func UpdateOpusArtwork(path string, artwork *Artwork) error {
	raw, err := ReadOpusCommentsList(path)
	if err != nil {
		return err
	}

	filtered := make([]string, 0, len(raw)+1)
	for _, c := range raw {
		upper := strings.ToUpper(c)
		if strings.HasPrefix(upper, FieldPicture+"=") || strings.HasPrefix(upper, "COVERART=") {
			continue
		}
		filtered = append(filtered, c)
	}

	if artwork != nil {
		filtered = append(filtered, formatComment(FieldPicture, artwork.MetadataBlockPicture()))
	}

	return writeOpusComments(path, filtered)
}

func cutComment(comment string) (string, string, bool) {
	for i := 0; i < len(comment); i++ {
		if comment[i] == '=' {
			return normaliseKey(comment[:i]), comment[i+1:], true
		}
	}
	return "", "", false
}

func normaliseKey(key string) string {
	out := make([]byte, len(key))
	for i := 0; i < len(key); i++ {
		c := key[i]
		if c >= 'a' && c <= 'z' {
			c -= 'a' - 'A'
		}
		out[i] = c
	}
	return string(out)
}

// formatComment renders one Vorbis comment entry.
func formatComment(key, value string) string {
	return fmt.Sprintf("%s=%s", key, value)
}
