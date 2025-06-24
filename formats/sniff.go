// --- formats/sniff.go ---

// Sniff determines the format of the data
func Sniff(r io.ReadSeeker, hint string) (Format, error) {
	// Read first 16 bytes for magic number detection
	header := make([]byte, 16)
	n, err := r.Read(header)
	if err != nil && err != io.EOF {
		return FormatUnknown, err
	}
	header = header[:n]

	// Reset position
	r.Seek(0, io.SeekStart)

	// Check magic numbers
	switch {
	case len(header) >= 3 && header[0] == 0xFF && header[1] == 0xD8 && header[2] == 0xFF:
		return FormatJPEG, nil

	case len(header) >= 8 && string(header[:8]) == "\x89PNG\r\n\x1a\n":
		return FormatPNG, nil

	case len(header) >= 3 && header[0] == 'I' && header[1] == 'D' && header[2] == '3':
		return FormatMP3, nil

	case len(header) >= 12 && (string(header[4:8]) == "ftyp" || string(header[4:8]) == "moov"):
		return FormatMP4, nil

	case len(header) >= 4 && string(header[:4]) == "%PDF":
		return FormatPDF, nil

	default:
		return FormatUnknown, nil
	}
}