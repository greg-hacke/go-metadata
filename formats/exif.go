package formats

import (
	"encoding/binary"
	"fmt"
	"io"

	"greg-hacke/go-metadata/meta"
	"greg-hacke/go-metadata/tags"
)

func init() {
	RegisterParser(FormatJPEG, &exifParser{})
}

type exifParser struct{}

func (p *exifParser) Parse(r io.ReadSeeker) ([]meta.Field, error) {
	// Find EXIF data in JPEG
	exifData, err := findEXIFData(r)
	if err != nil {
		return nil, err
	}

	if exifData == nil {
		return nil, nil // No EXIF data found
	}

	return parseEXIFData(exifData)
}

// findEXIFData locates EXIF APP1 segment in JPEG
func findEXIFData(r io.ReadSeeker) ([]byte, error) {
	// Skip SOI marker
	r.Seek(2, io.SeekStart)

	for {
		var marker [2]byte
		if _, err := r.Read(marker[:]); err != nil {
			return nil, err
		}

		if marker[0] != 0xFF {
			return nil, fmt.Errorf("invalid JPEG marker")
		}

		// Read segment length
		var length uint16
		if err := binary.Read(r, binary.BigEndian, &length); err != nil {
			return nil, err
		}

		// APP1 marker (0xFFE1) contains EXIF
		if marker[1] == 0xE1 {
			// Read segment data
			data := make([]byte, length-2)
			if _, err := r.Read(data); err != nil {
				return nil, err
			}

			// Check for "Exif\x00\x00" header
			if len(data) >= 6 && string(data[:6]) == "Exif\x00\x00" {
				return data[6:], nil // Return TIFF data
			}
		}

		// Skip segment
		if _, err := r.Seek(int64(length-2), io.SeekCurrent); err != nil {
			return nil, err
		}

		// Check for image data start
		if marker[1] == 0xDA {
			break
		}
	}

	return nil, nil
}

// parseEXIFData parses TIFF-formatted EXIF data
func parseEXIFData(data []byte) ([]meta.Field, error) {
	if len(data) < 8 {
		return nil, fmt.Errorf("EXIF data too short")
	}

	// Check byte order
	var byteOrder binary.ByteOrder
	if data[0] == 'I' && data[1] == 'I' {
		byteOrder = binary.LittleEndian
	} else if data[0] == 'M' && data[1] == 'M' {
		byteOrder = binary.BigEndian
	} else {
		return nil, fmt.Errorf("invalid TIFF byte order")
	}

	// Read IFD offset
	ifdOffset := byteOrder.Uint32(data[4:8])

	var fields []meta.Field

	// Parse IFD0 (main image tags)
	if err := parseIFD(data, ifdOffset, byteOrder, "EXIF", &fields); err != nil {
		return nil, err
	}

	return fields, nil
}

// parseIFD parses an Image File Directory
func parseIFD(data []byte, offset uint32, order binary.ByteOrder, namespace string, fields *[]meta.Field) error {
	if int(offset+2) > len(data) {
		return fmt.Errorf("IFD offset out of bounds")
	}

	// Read number of entries
	numEntries := order.Uint16(data[offset : offset+2])
	offset += 2

	// Parse each entry
	for i := 0; i < int(numEntries); i++ {
		if int(offset+12) > len(data) {
			break
		}

		tagID := order.Uint16(data[offset : offset+2])
		// dataType := order.Uint16(data[offset+2 : offset+4])
		// count := order.Uint32(data[offset+4 : offset+8])
		valueOffset := order.Uint32(data[offset+8 : offset+12])

		// Look up tag definition
		tagKey := fmt.Sprintf("0x%04x", tagID)
		if tagDef, ok := tags.GetTag("EXIF", tagKey); ok {
			// For demo, just add the tag with offset as value
			*fields = append(*fields, meta.Field{
				Namespace:   namespace,
				Key:         tagDef.Name,
				Value:       valueOffset, // In real implementation, decode based on type
				Description: tagDef.Description,
			})
		}

		offset += 12
	}

	return nil
}
