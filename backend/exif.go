package backend

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// filenamePattern holds a compiled regex and whether it captures a full time component.
type filenamePattern struct {
	re       *regexp.Regexp
	hasTime  bool
	isUnixMs bool
}

var filenameTimestampPatterns = []filenamePattern{
	// YYYYMMDD[_-]HHMMSS — e.g. 20240709_182027.mp4, PXL_20231123_182518628.jpg
	{regexp.MustCompile(`(\d{4})(\d{2})(\d{2})[_-](\d{2})(\d{2})(\d{2})\d*`), true, false},
	// YYYY-MM-DD[sep HHMMSS] — e.g. 2022-10-24-150226287.mp4, Screenshot 2026-02-13 093505.png
	{regexp.MustCompile(`(\d{4})-(\d{1,2})-(\d{1,2})(?:[ _-](\d{2})(\d{2})(\d{2})\d*)?`), false, false},
	// [non-digit]YYYYMMDDHHMMSS[non-digit] — e.g. lv_7324034615860006160_20240617193045.mp4
	{regexp.MustCompile(`(?:^|[^0-9])(\d{4})(\d{2})(\d{2})(\d{2})(\d{2})(\d{2})(?:[^0-9]|$)`), true, false},
	// Unix milliseconds — e.g. FaceApp_1658848332262.jpg (covers 2001–2033)
	{regexp.MustCompile(`(?:^|[^0-9])(1\d{12})(?:[^0-9]|$)`), true, true},
}

// extractTimestampFromFilename tries each pattern in priority order.
// Returns the extracted time and true on success.
func extractTimestampFromFilename(filename string) (time.Time, bool) {
	base := filepath.Base(filename)

	for _, pat := range filenameTimestampPatterns {
		m := pat.re.FindStringSubmatch(base)
		if m == nil {
			continue
		}

		if pat.isUnixMs {
			ms, err := strconv.ParseInt(m[1], 10, 64)
			if err != nil {
				continue
			}
			t := time.UnixMilli(ms).In(time.Local)
			if t.Year() < 1990 || t.Year() > time.Now().Year()+1 {
				continue
			}
			return t, true
		}

		year, month, day := m[1], m[2], m[3]
		hour, min, sec := "12", "00", "00"

		if pat.hasTime {
			hour, min, sec = m[4], m[5], m[6]
		} else if len(m) >= 7 && m[4] != "" {
			hour, min, sec = m[4], m[5], m[6]
		}

		pad2 := func(s string) string {
			if len(s) < 2 {
				return "0" + s
			}
			return s
		}

		t, err := time.ParseInLocation("20060102 150405",
			year+pad2(month)+pad2(day)+" "+pad2(hour)+min+sec, time.Local)
		if err != nil {
			continue
		}
		if t.Year() < 1990 || t.Year() > time.Now().Year()+1 {
			continue
		}
		return t, true
	}

	return time.Time{}, false
}

// canonicalExt normalises a lowercase file extension to its canonical form
// so that equivalent extensions compare equal (.jpeg→.jpg, .tif→.tiff).
func canonicalExt(ext string) string {
	switch ext {
	case ".jpeg":
		return ".jpg"
	case ".tif":
		return ".tiff"
	}
	return ext
}

// detectFormatExtension reads the file's magic bytes and returns the canonical
// extension (with leading dot, lowercase) for the detected format, or "" if
// the format is unrecognised.
func detectFormatExtension(filePath string) string {
	f, err := os.Open(filePath)
	if err != nil {
		return ""
	}
	defer f.Close()

	var h [16]byte
	n, _ := f.Read(h[:])
	if n < 4 {
		return ""
	}

	// JPEG
	if n >= 2 && h[0] == 0xFF && h[1] == 0xD8 {
		return ".jpg"
	}
	// PNG
	if n >= 4 && h[0] == 0x89 && h[1] == 0x50 && h[2] == 0x4E && h[3] == 0x47 {
		return ".png"
	}
	// GIF
	if n >= 4 && h[0] == 0x47 && h[1] == 0x49 && h[2] == 0x46 && h[3] == 0x38 {
		return ".gif"
	}
	// BMP
	if n >= 2 && h[0] == 0x42 && h[1] == 0x4D {
		return ".bmp"
	}
	// TIFF (little-endian or big-endian)
	if n >= 4 && ((h[0] == 0x49 && h[1] == 0x49 && h[2] == 0x2A && h[3] == 0x00) ||
		(h[0] == 0x4D && h[1] == 0x4D && h[2] == 0x00 && h[3] == 0x2A)) {
		return ".tiff"
	}
	// RIFF container (WebP, AVI)
	if n >= 12 && h[0] == 'R' && h[1] == 'I' && h[2] == 'F' && h[3] == 'F' {
		sub := string(h[8:12])
		switch sub {
		case "WEBP":
			return ".webp"
		case "AVI ":
			return ".avi"
		}
		return ""
	}
	// ISO Base Media (MP4, MOV, HEIC, HEIF, 3GP, …)
	if n >= 12 && h[4] == 'f' && h[5] == 't' && h[6] == 'y' && h[7] == 'p' {
		brand := string(h[8:12])
		switch brand {
		case "heic", "heix", "hevc", "hevx", "heim", "heis", "hevm", "hevs":
			return ".heic"
		case "mif1", "msf1":
			return ".heif"
		case "qt  ":
			return ".mov"
		case "M4V ", "M4VH", "M4VP":
			return ".m4v"
		case "3gp4", "3gp5", "3gp6", "3ge6", "3ge7", "3gg6", "3gp2":
			return ".3gp"
		case "3g2a", "3g2b", "3g2c":
			return ".3g2"
		default:
			return ".mp4"
		}
	}
	// Matroska / WebM (EBML)
	if n >= 4 && h[0] == 0x1A && h[1] == 0x45 && h[2] == 0xDF && h[3] == 0xA3 {
		return ".mkv"
	}

	return ""
}

// RenameToCorrectExtension detects the file's actual format from magic bytes and
// renames it when the extension doesn't match. Returns the (possibly new) path
// and true if a rename was performed.
func RenameToCorrectExtension(filePath string) (string, bool) {
	detected := detectFormatExtension(filePath)
	if detected == "" {
		return filePath, false
	}
	rawExt := filepath.Ext(filePath)
	if canonicalExt(strings.ToLower(rawExt)) == detected {
		return filePath, false
	}
	newPath := filePath[:len(filePath)-len(rawExt)] + detected
	if err := os.Rename(filePath, newPath); err != nil {
		return filePath, false
	}
	return newPath, true
}
