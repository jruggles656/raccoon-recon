package scanner

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"net/http"
	"strings"
)

// FileMetaResult holds a single extracted metadata key-value pair.
type FileMetaResult struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// ExtractFileMetadata detects the file type and extracts metadata.
func ExtractFileMetadata(filename string, data []byte) ([]FileMetaResult, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty file")
	}

	mimeType := http.DetectContentType(data)
	results := []FileMetaResult{
		{Key: "filename", Value: filename},
		{Key: "file_size", Value: formatFileSize(len(data))},
		{Key: "mime_type", Value: mimeType},
	}

	switch {
	case strings.HasPrefix(mimeType, "image/jpeg"):
		results = append(results, extractJPEGMetadata(data)...)
	case strings.HasPrefix(mimeType, "image/png"):
		results = append(results, extractPNGMetadata(data)...)
	case mimeType == "application/pdf":
		results = append(results, extractPDFMetadata(data)...)
	default:
		// Try to decode as generic image for dimensions
		if img, _, err := image.DecodeConfig(bytes.NewReader(data)); err == nil {
			results = append(results,
				FileMetaResult{Key: "width", Value: fmt.Sprintf("%d px", img.Width)},
				FileMetaResult{Key: "height", Value: fmt.Sprintf("%d px", img.Height)},
			)
		}
	}

	return results, nil
}

func formatFileSize(n int) string {
	if n < 1024 {
		return fmt.Sprintf("%d bytes", n)
	}
	if n < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	}
	return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
}

// --- JPEG / EXIF ---

func extractJPEGMetadata(data []byte) []FileMetaResult {
	var results []FileMetaResult

	// Get dimensions via image package
	if img, _, err := image.DecodeConfig(bytes.NewReader(data)); err == nil {
		results = append(results,
			FileMetaResult{Key: "width", Value: fmt.Sprintf("%d px", img.Width)},
			FileMetaResult{Key: "height", Value: fmt.Sprintf("%d px", img.Height)},
		)
	}

	// Find APP1 EXIF segment (0xFF 0xE1)
	exifData := findEXIFSegment(data)
	if exifData == nil {
		return results
	}

	results = append(results, parseEXIF(exifData)...)
	return results
}

func findEXIFSegment(data []byte) []byte {
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return nil // Not a JPEG
	}

	offset := 2
	for offset+4 < len(data) {
		if data[offset] != 0xFF {
			offset++
			continue
		}
		marker := data[offset+1]
		if marker == 0xE1 { // APP1
			segLen := int(binary.BigEndian.Uint16(data[offset+2 : offset+4]))
			if offset+2+segLen > len(data) {
				return nil
			}
			segment := data[offset+4 : offset+2+segLen]
			// Check for "Exif\0\0" header
			if len(segment) > 6 && string(segment[:4]) == "Exif" && segment[4] == 0 && segment[5] == 0 {
				return segment[6:] // Return TIFF data after "Exif\0\0"
			}
			return nil
		}
		if marker == 0xDA { // Start of scan — stop looking
			break
		}
		if offset+4 > len(data) {
			break
		}
		segLen := int(binary.BigEndian.Uint16(data[offset+2 : offset+4]))
		offset += 2 + segLen
	}
	return nil
}

// EXIF tag IDs we care about
var exifTagNames = map[uint16]string{
	0x010F: "camera_make",
	0x0110: "camera_model",
	0x0112: "orientation",
	0x011A: "x_resolution",
	0x011B: "y_resolution",
	0x0131: "software",
	0x0132: "date_modified",
	0x0213: "ycbcr_positioning",
	0x8769: "_exif_ifd",    // Pointer to Exif sub-IFD
	0x8825: "_gps_ifd",     // Pointer to GPS IFD
	0x9003: "date_original",
	0x9004: "date_digitized",
	0x829A: "exposure_time",
	0x829D: "f_number",
	0x8827: "iso_speed",
	0x920A: "focal_length",
	0xA001: "color_space",
	0xA002: "exif_width",
	0xA003: "exif_height",
	0xA405: "focal_length_35mm",
}

// GPS tag IDs
var gpsTagNames = map[uint16]string{
	0x0001: "gps_lat_ref",
	0x0002: "gps_latitude",
	0x0003: "gps_lon_ref",
	0x0004: "gps_longitude",
	0x0005: "gps_alt_ref",
	0x0006: "gps_altitude",
}

func parseEXIF(tiffData []byte) []FileMetaResult {
	if len(tiffData) < 8 {
		return nil
	}

	var bo binary.ByteOrder
	if string(tiffData[:2]) == "II" {
		bo = binary.LittleEndian
	} else if string(tiffData[:2]) == "MM" {
		bo = binary.BigEndian
	} else {
		return nil
	}

	// Verify TIFF magic (42)
	if bo.Uint16(tiffData[2:4]) != 42 {
		return nil
	}

	ifdOffset := int(bo.Uint32(tiffData[4:8]))
	results := parseIFD(tiffData, bo, ifdOffset, exifTagNames)

	// Parse sub-IFDs
	var gpsResults []FileMetaResult
	var exifSubResults []FileMetaResult
	filtered := make([]FileMetaResult, 0, len(results))
	for _, r := range results {
		if r.Key == "_exif_ifd" {
			offset := parseUint(r.Value)
			if offset > 0 && offset < len(tiffData) {
				exifSubResults = parseIFD(tiffData, bo, offset, exifTagNames)
			}
		} else if r.Key == "_gps_ifd" {
			offset := parseUint(r.Value)
			if offset > 0 && offset < len(tiffData) {
				gpsResults = parseIFD(tiffData, bo, offset, gpsTagNames)
			}
		} else {
			filtered = append(filtered, r)
		}
	}

	filtered = append(filtered, exifSubResults...)

	// Convert GPS data to human-readable coordinates
	filtered = append(filtered, convertGPSResults(gpsResults)...)

	return filtered
}

func parseIFD(data []byte, bo binary.ByteOrder, offset int, tagNames map[uint16]string) []FileMetaResult {
	if offset+2 > len(data) {
		return nil
	}

	numEntries := int(bo.Uint16(data[offset : offset+2]))
	if numEntries > 200 {
		return nil // sanity check
	}

	var results []FileMetaResult
	for i := 0; i < numEntries; i++ {
		entryOffset := offset + 2 + i*12
		if entryOffset+12 > len(data) {
			break
		}

		tag := bo.Uint16(data[entryOffset : entryOffset+2])
		dataType := bo.Uint16(data[entryOffset+2 : entryOffset+4])
		count := int(bo.Uint32(data[entryOffset+4 : entryOffset+8]))
		valueBytes := data[entryOffset+8 : entryOffset+12]

		name, known := tagNames[tag]
		if !known {
			continue
		}

		value := readEXIFValue(data, bo, dataType, count, valueBytes)
		if value != "" {
			results = append(results, FileMetaResult{Key: name, Value: value})
		}
	}

	return results
}

func readEXIFValue(data []byte, bo binary.ByteOrder, dataType uint16, count int, valueBytes []byte) string {
	// Calculate total size
	typeSize := map[uint16]int{1: 1, 2: 1, 3: 2, 4: 4, 5: 8, 7: 1, 9: 4, 10: 8}
	size := typeSize[dataType] * count
	if size == 0 {
		return ""
	}

	var valData []byte
	if size <= 4 {
		valData = valueBytes[:size]
	} else {
		offset := int(bo.Uint32(valueBytes))
		if offset+size > len(data) || offset < 0 {
			return ""
		}
		valData = data[offset : offset+size]
	}

	switch dataType {
	case 2: // ASCII
		s := string(valData)
		s = strings.TrimRight(s, "\x00 ")
		if len(s) > 200 {
			s = s[:200]
		}
		return s
	case 3: // SHORT (uint16)
		if len(valData) >= 2 {
			return fmt.Sprintf("%d", bo.Uint16(valData))
		}
	case 4: // LONG (uint32)
		if len(valData) >= 4 {
			return fmt.Sprintf("%d", bo.Uint32(valData))
		}
	case 5: // RATIONAL (two uint32: numerator/denominator)
		if count == 1 && len(valData) >= 8 {
			num := bo.Uint32(valData[0:4])
			den := bo.Uint32(valData[4:8])
			if den == 0 {
				return "0"
			}
			if den == 1 {
				return fmt.Sprintf("%d", num)
			}
			return fmt.Sprintf("%.4g", float64(num)/float64(den))
		}
		// Multiple rationals (e.g. GPS coordinates: 3 rationals)
		if count > 1 && len(valData) >= count*8 {
			var parts []string
			for i := 0; i < count; i++ {
				num := bo.Uint32(valData[i*8 : i*8+4])
				den := bo.Uint32(valData[i*8+4 : i*8+8])
				if den == 0 {
					parts = append(parts, "0")
				} else {
					parts = append(parts, fmt.Sprintf("%.6g", float64(num)/float64(den)))
				}
			}
			return strings.Join(parts, ", ")
		}
	case 9: // SLONG (int32)
		if len(valData) >= 4 {
			return fmt.Sprintf("%d", int32(bo.Uint32(valData)))
		}
	case 10: // SRATIONAL
		if len(valData) >= 8 {
			num := int32(bo.Uint32(valData[0:4]))
			den := int32(bo.Uint32(valData[4:8]))
			if den == 0 {
				return "0"
			}
			return fmt.Sprintf("%.4g", float64(num)/float64(den))
		}
	}
	return ""
}

func convertGPSResults(gpsResults []FileMetaResult) []FileMetaResult {
	var latRef, lonRef, latVal, lonVal string
	var altRef, altVal string
	for _, r := range gpsResults {
		switch r.Key {
		case "gps_lat_ref":
			latRef = r.Value
		case "gps_latitude":
			latVal = r.Value
		case "gps_lon_ref":
			lonRef = r.Value
		case "gps_longitude":
			lonVal = r.Value
		case "gps_alt_ref":
			altRef = r.Value
		case "gps_altitude":
			altVal = r.Value
		}
	}

	var results []FileMetaResult
	if latVal != "" && lonVal != "" {
		lat := dmsToDecimal(latVal, latRef)
		lon := dmsToDecimal(lonVal, lonRef)
		results = append(results, FileMetaResult{
			Key:   "gps_coordinates",
			Value: fmt.Sprintf("%.6f, %.6f", lat, lon),
		})
	}
	if altVal != "" {
		alt := altVal
		if altRef == "1" {
			alt = "-" + alt
		}
		results = append(results, FileMetaResult{Key: "gps_altitude", Value: alt + " m"})
	}
	return results
}

func dmsToDecimal(dms string, ref string) float64 {
	// dms is comma-separated rationals: "degrees, minutes, seconds"
	parts := strings.Split(dms, ", ")
	if len(parts) < 3 {
		return 0
	}
	var vals [3]float64
	for i, p := range parts[:3] {
		fmt.Sscanf(strings.TrimSpace(p), "%f", &vals[i])
	}
	dec := vals[0] + vals[1]/60 + vals[2]/3600
	if ref == "S" || ref == "W" {
		dec = -dec
	}
	// Round to 6 decimal places
	dec = math.Round(dec*1e6) / 1e6
	return dec
}

func parseUint(s string) int {
	var n int
	fmt.Sscanf(s, "%d", &n)
	return n
}

// --- PNG ---

func extractPNGMetadata(data []byte) []FileMetaResult {
	var results []FileMetaResult

	// Get dimensions
	if img, _, err := image.DecodeConfig(bytes.NewReader(data)); err == nil {
		results = append(results,
			FileMetaResult{Key: "width", Value: fmt.Sprintf("%d px", img.Width)},
			FileMetaResult{Key: "height", Value: fmt.Sprintf("%d px", img.Height)},
		)
	}

	// Parse tEXt chunks
	// PNG structure: 8-byte header, then chunks: [4-byte length][4-byte type][data][4-byte CRC]
	if len(data) < 8 {
		return results
	}

	offset := 8 // Skip PNG header
	for offset+8 < len(data) {
		chunkLen := int(binary.BigEndian.Uint32(data[offset : offset+4]))
		chunkType := string(data[offset+4 : offset+8])

		if chunkLen < 0 || offset+12+chunkLen > len(data) {
			break
		}

		chunkData := data[offset+8 : offset+8+chunkLen]

		switch chunkType {
		case "tEXt":
			// null-separated keyword and text
			if idx := bytes.IndexByte(chunkData, 0); idx >= 0 {
				key := string(chunkData[:idx])
				val := string(chunkData[idx+1:])
				if len(val) > 500 {
					val = val[:500]
				}
				results = append(results, FileMetaResult{Key: pngKeyName(key), Value: val})
			}
		case "iTXt":
			// keyword \0 compression_flag compression_method language \0 translated_keyword \0 text
			if idx := bytes.IndexByte(chunkData, 0); idx >= 0 {
				key := string(chunkData[:idx])
				rest := chunkData[idx+1:]
				// Skip compression flag, method, language tag, translated keyword
				// Find the text after three null bytes
				nullCount := 0
				textStart := 0
				for i, b := range rest {
					if b == 0 {
						nullCount++
						if nullCount >= 3 {
							textStart = i + 1
							break
						}
					}
				}
				if textStart > 0 && textStart < len(rest) {
					val := string(rest[textStart:])
					if len(val) > 500 {
						val = val[:500]
					}
					results = append(results, FileMetaResult{Key: pngKeyName(key), Value: val})
				}
			}
		case "IEND":
			break
		}

		offset += 12 + chunkLen // length + type + data + CRC
	}

	return results
}

func pngKeyName(key string) string {
	lower := strings.ToLower(key)
	switch lower {
	case "author", "artist":
		return "author"
	case "description", "comment":
		return lower
	case "copyright":
		return "copyright"
	case "creation time", "create-date":
		return "creation_time"
	case "software":
		return "software"
	case "title":
		return "title"
	default:
		return strings.ReplaceAll(lower, " ", "_")
	}
}

// --- PDF ---

func extractPDFMetadata(data []byte) []FileMetaResult {
	var results []FileMetaResult
	content := string(data)

	// Count pages: /Type /Page (but not /Type /Pages)
	pageCount := 0
	idx := 0
	for {
		pos := strings.Index(content[idx:], "/Type /Page")
		if pos == -1 {
			break
		}
		absPos := idx + pos
		after := absPos + len("/Type /Page")
		if after < len(content) {
			nextChar := content[after]
			// Only count if NOT followed by 's' (which would be /Type /Pages)
			if nextChar != 's' && nextChar != 'S' {
				pageCount++
			}
		}
		idx = absPos + 1
	}
	// Also try /Type/Page (without space)
	idx = 0
	for {
		pos := strings.Index(content[idx:], "/Type/Page")
		if pos == -1 {
			break
		}
		absPos := idx + pos
		after := absPos + len("/Type/Page")
		if after < len(content) {
			nextChar := content[after]
			if nextChar != 's' && nextChar != 'S' {
				pageCount++
			}
		}
		idx = absPos + 1
	}
	if pageCount > 0 {
		results = append(results, FileMetaResult{Key: "page_count", Value: fmt.Sprintf("%d", pageCount)})
	}

	// Parse /Info dictionary entries
	pdfFields := []struct {
		tag  string
		name string
	}{
		{"/Title", "title"},
		{"/Author", "author"},
		{"/Subject", "subject"},
		{"/Creator", "creator"},
		{"/Producer", "producer"},
		{"/CreationDate", "creation_date"},
		{"/ModDate", "modification_date"},
		{"/Keywords", "keywords"},
	}

	for _, f := range pdfFields {
		if val := extractPDFString(content, f.tag); val != "" {
			results = append(results, FileMetaResult{Key: f.name, Value: val})
		}
	}

	// Get PDF version from header
	if len(content) > 8 && strings.HasPrefix(content, "%PDF-") {
		endIdx := strings.Index(content[:20], "\n")
		if endIdx == -1 {
			endIdx = 8
		}
		version := strings.TrimSpace(content[5:endIdx])
		if len(version) > 0 && len(version) < 10 {
			results = append(results, FileMetaResult{Key: "pdf_version", Value: version})
		}
	}

	return results
}

func extractPDFString(content, tag string) string {
	idx := strings.Index(content, tag)
	if idx == -1 {
		return ""
	}

	rest := content[idx+len(tag):]
	rest = strings.TrimLeft(rest, " ")

	if len(rest) == 0 {
		return ""
	}

	// Parenthesized string: /Title (Some Title)
	if rest[0] == '(' {
		depth := 0
		end := -1
		for i, c := range rest {
			if c == '(' {
				depth++
			} else if c == ')' {
				depth--
				if depth == 0 {
					end = i
					break
				}
			}
		}
		if end > 1 {
			val := rest[1:end]
			// Handle PDF date format: D:20210101120000
			if strings.HasPrefix(val, "D:") {
				val = formatPDFDate(val)
			}
			if len(val) > 500 {
				val = val[:500]
			}
			return val
		}
	}

	// Hex string: /Title <FEFF0048...>
	if rest[0] == '<' {
		end := strings.Index(rest, ">")
		if end > 1 {
			hex := rest[1:end]
			// Try to decode UTF-16BE (starts with FEFF)
			if strings.HasPrefix(strings.ToUpper(hex), "FEFF") {
				decoded := decodeUTF16BEHex(hex[4:])
				if decoded != "" {
					return decoded
				}
			}
			return hex
		}
	}

	return ""
}

func formatPDFDate(d string) string {
	// D:YYYYMMDDHHmmSS+HH'mm'
	d = strings.TrimPrefix(d, "D:")
	if len(d) < 8 {
		return d
	}
	result := d[:4] + "-" + d[4:6] + "-" + d[6:8]
	if len(d) >= 14 {
		result += " " + d[8:10] + ":" + d[10:12] + ":" + d[12:14]
	}
	return result
}

func decodeUTF16BEHex(hex string) string {
	hex = strings.ReplaceAll(hex, " ", "")
	if len(hex)%4 != 0 {
		return ""
	}
	var sb strings.Builder
	for i := 0; i+3 < len(hex); i += 4 {
		var cp uint16
		fmt.Sscanf(hex[i:i+4], "%04X", &cp)
		sb.WriteRune(rune(cp))
	}
	return sb.String()
}
