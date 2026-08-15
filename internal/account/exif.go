package account

import (
	"encoding/binary"
	"image"
	"image/color"
)

// colorFrom builds an 8-bit RGBA from the 16-bit components RGBA() returns.
func colorFrom(r, g, b, a uint64) color.RGBA {
	return color.RGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: uint8(a >> 8)}
}

// EXIF orientation values, as defined by the TIFF tag 0x0112.
const (
	orientNormal      = 1
	orientFlipH       = 2
	orientRotate180   = 3
	orientFlipV       = 4
	orientTranspose   = 5
	orientRotate90    = 6
	orientTransverse  = 7
	orientRotate270   = 8
	exifOrientationID = 0x0112
)

// exifOrientation reads the orientation tag out of a JPEG, returning
// orientNormal when there is no EXIF block or it cannot be parsed.
//
// This exists because image/jpeg decodes pixels exactly as stored and ignores
// the tag entirely. Phones almost always record a landscape sensor readout plus
// an orientation flag rather than rotating the pixels, so without this every
// portrait photo uploaded from a phone arrives lying on its side.
//
// Deliberately hand-rolled and deliberately small: it walks to the APP1 marker,
// reads the TIFF header, and scans the first IFD for one tag. Anything
// unexpected returns orientNormal rather than an error, because a missing or
// malformed orientation is not a reason to reject someone's photo.
func exifOrientation(raw []byte) int {
	// Every JPEG starts with SOI (FFD8); segments follow as FF <marker> <len>.
	if len(raw) < 4 || raw[0] != 0xFF || raw[1] != 0xD8 {
		return orientNormal
	}

	for i := 2; i+4 <= len(raw); {
		if raw[i] != 0xFF {
			return orientNormal
		}
		marker := raw[i+1]
		// Start of scan: image data begins, so there is no metadata left.
		if marker == 0xDA {
			return orientNormal
		}
		segLen := int(binary.BigEndian.Uint16(raw[i+2 : i+4]))
		if segLen < 2 || i+2+segLen > len(raw) {
			return orientNormal
		}
		if marker == 0xE1 { // APP1, where EXIF lives
			if o, ok := parseExifAPP1(raw[i+4 : i+2+segLen]); ok {
				return o
			}
		}
		i += 2 + segLen
	}
	return orientNormal
}

func parseExifAPP1(seg []byte) (int, bool) {
	const header = "Exif\x00\x00"
	if len(seg) < len(header)+8 || string(seg[:len(header)]) != header {
		return 0, false
	}
	tiff := seg[len(header):]

	var order binary.ByteOrder
	switch {
	case tiff[0] == 'I' && tiff[1] == 'I':
		order = binary.LittleEndian
	case tiff[0] == 'M' && tiff[1] == 'M':
		order = binary.BigEndian
	default:
		return 0, false
	}
	if order.Uint16(tiff[2:4]) != 42 { // TIFF magic
		return 0, false
	}

	ifdOffset := int(order.Uint32(tiff[4:8]))
	if ifdOffset < 8 || ifdOffset+2 > len(tiff) {
		return 0, false
	}

	count := int(order.Uint16(tiff[ifdOffset : ifdOffset+2]))
	entry := ifdOffset + 2
	for n := 0; n < count; n++ {
		if entry+12 > len(tiff) {
			return 0, false
		}
		if order.Uint16(tiff[entry:entry+2]) == exifOrientationID {
			// A SHORT value is stored in the first two bytes of the 4-byte
			// value field, in the file's byte order.
			v := int(order.Uint16(tiff[entry+8 : entry+10]))
			if v >= orientNormal && v <= orientRotate270 {
				return v, true
			}
			return 0, false
		}
		entry += 12
	}
	return 0, false
}

// applyOrientation returns src rotated and flipped so it displays upright.
func applyOrientation(src image.Image, orientation int) image.Image {
	if orientation <= orientNormal || orientation > orientRotate270 {
		return src
	}

	b := src.Bounds()
	w, h := b.Dx(), b.Dy()

	// The transposing orientations swap the output's width and height.
	outW, outH := w, h
	switch orientation {
	case orientTranspose, orientRotate90, orientTransverse, orientRotate270:
		outW, outH = h, w
	}

	out := image.NewRGBA(image.Rect(0, 0, outW, outH))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var dx, dy int
			switch orientation {
			case orientFlipH:
				dx, dy = w-1-x, y
			case orientRotate180:
				dx, dy = w-1-x, h-1-y
			case orientFlipV:
				dx, dy = x, h-1-y
			case orientTranspose:
				dx, dy = y, x
			case orientRotate90:
				dx, dy = h-1-y, x
			case orientTransverse:
				dx, dy = h-1-y, w-1-x
			case orientRotate270:
				dx, dy = y, w-1-x
			}
			out.Set(dx, dy, src.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return out
}
