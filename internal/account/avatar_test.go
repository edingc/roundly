package account

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

// solidJPEG encodes a w×h image, optionally with a distinguishing stripe down
// the left edge so an orientation change is detectable.
func solidJPEG(t *testing.T, w, h int, stripe bool) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := color.RGBA{R: 20, G: 120, B: 60, A: 255}
			if stripe && x < w/8 {
				c = color.RGBA{R: 240, G: 40, B: 40, A: 255}
			}
			img.SetRGBA(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 92}); err != nil {
		t.Fatalf("encode fixture: %v", err)
	}
	return buf.Bytes()
}

func solidPNG(t *testing.T, w, h int, alpha uint8) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, color.RGBA{R: 10, G: 10, B: 200, A: alpha})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode fixture: %v", err)
	}
	return buf.Bytes()
}

func TestProcessAvatarProducesSquareJPEG(t *testing.T) {
	tests := []struct {
		name     string
		w, h     int
		wantSide int
	}{
		{"landscape downscales to the cap", 1000, 400, 256},
		{"portrait downscales to the cap", 400, 1000, 256},
		{"already square", 512, 512, 256},
		// Never upscaled: enlarging a small source only makes it blurry.
		{"smaller than the cap is left alone", 100, 100, 100},
		{"small landscape crops to its short side", 200, 96, 96},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out, err := processAvatar(solidJPEG(t, tc.w, tc.h, false))
			if err != nil {
				t.Fatalf("processAvatar: %v", err)
			}

			cfg, format, err := image.DecodeConfig(bytes.NewReader(out))
			if err != nil {
				t.Fatalf("decode result: %v", err)
			}
			if format != "jpeg" {
				t.Errorf("format = %q, want jpeg", format)
			}
			if cfg.Width != tc.wantSide || cfg.Height != tc.wantSide {
				t.Errorf("size = %dx%d, want %dx%d", cfg.Width, cfg.Height, tc.wantSide, tc.wantSide)
			}
		})
	}
}

func TestProcessAvatarRejectsNonImages(t *testing.T) {
	tests := []struct {
		name string
		body []byte
	}{
		// The sniffing test: a PDF is still a PDF however the multipart part
		// labels itself, because the part's Content-Type is never consulted.
		{"a PDF", []byte("%PDF-1.7\n%\xe2\xe3\xcf\xd3\n" + string(make([]byte, 600)))},
		{"a GIF", append([]byte("GIF89a"), make([]byte, 600)...)},
		{"plain text", []byte("this is definitely not an image, not even a little bit, honestly")},
		{"empty", []byte{}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := processAvatar(tc.body); err == nil {
				t.Fatal("expected a rejection, got none")
			}
		})
	}
}

func TestProcessAvatarRejectsOutOfRangeDimensions(t *testing.T) {
	t.Run("too small", func(t *testing.T) {
		if _, err := processAvatar(solidJPEG(t, 32, 32, false)); err == nil {
			t.Fatal("expected a rejection for a 32x32 source")
		}
	})

	t.Run("too many pixels", func(t *testing.T) {
		// Built as a header rather than a real image: the point is that the
		// size check happens on DecodeConfig, before any pixels are allocated.
		var buf bytes.Buffer
		if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 9000, 10))); err != nil {
			t.Fatalf("encode: %v", err)
		}
		if _, err := processAvatar(buf.Bytes()); err == nil {
			t.Fatal("expected a rejection for a 9000px side")
		}
	})
}

// The privacy guarantee: re-encoding from decoded pixels leaves no path for
// metadata, so GPS coordinates and camera serials cannot survive an upload.
func TestProcessAvatarStripsEXIF(t *testing.T) {
	withExif := jpegWithOrientation(t, solidJPEG(t, 600, 400, true), orientRotate90)
	if !bytes.Contains(withExif, []byte("Exif\x00\x00")) {
		t.Fatal("fixture is broken: no EXIF header to strip")
	}

	out, err := processAvatar(withExif)
	if err != nil {
		t.Fatalf("processAvatar: %v", err)
	}
	if bytes.Contains(out, []byte("Exif\x00\x00")) {
		t.Error("output still carries an EXIF block")
	}
}

func TestExifOrientationIsParsed(t *testing.T) {
	for _, want := range []int{orientFlipH, orientRotate180, orientRotate90, orientRotate270} {
		fixture := jpegWithOrientation(t, solidJPEG(t, 200, 200, false), want)
		if got := exifOrientation(fixture); got != want {
			t.Errorf("exifOrientation = %d, want %d", got, want)
		}
	}
}

func TestApplyOrientationMovesPixels(t *testing.T) {
	// A 4x2 image with one white pixel in the top-left corner. Where that
	// corner lands is the whole test.
	src := image.NewRGBA(image.Rect(0, 0, 4, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 4; x++ {
			src.SetRGBA(x, y, color.RGBA{A: 255})
		}
	}
	src.SetRGBA(0, 0, color.RGBA{R: 255, G: 255, B: 255, A: 255})

	tests := []struct {
		name        string
		orientation int
		wantW       int
		wantH       int
		wantX       int
		wantY       int
	}{
		{"normal", orientNormal, 4, 2, 0, 0},
		{"flip horizontal", orientFlipH, 4, 2, 3, 0},
		{"rotate 180", orientRotate180, 4, 2, 3, 1},
		{"flip vertical", orientFlipV, 4, 2, 0, 1},
		// The transposing cases also swap the output's dimensions.
		{"rotate 90", orientRotate90, 2, 4, 1, 0},
		{"rotate 270", orientRotate270, 2, 4, 0, 3},
		{"transpose", orientTranspose, 2, 4, 0, 0},
		{"transverse", orientTransverse, 2, 4, 1, 3},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out := applyOrientation(src, tc.orientation)
			b := out.Bounds()
			if b.Dx() != tc.wantW || b.Dy() != tc.wantH {
				t.Fatalf("size = %dx%d, want %dx%d", b.Dx(), b.Dy(), tc.wantW, tc.wantH)
			}
			r, _, _, _ := out.At(tc.wantX, tc.wantY).RGBA()
			if r>>8 < 200 {
				t.Errorf("white corner is not at (%d,%d); found R=%d there", tc.wantX, tc.wantY, r>>8)
			}
		})
	}
}

func TestProcessAvatarAppliesOrientation(t *testing.T) {
	// Bands across the full width, so a centre crop keeps both of them: red on
	// top, green below. Rotated 90 degrees the split becomes left/right.
	img := image.NewRGBA(image.Rect(0, 0, 600, 200))
	for y := 0; y < 200; y++ {
		for x := 0; x < 600; x++ {
			c := color.RGBA{R: 20, G: 160, B: 60, A: 255}
			if y < 100 {
				c = color.RGBA{R: 240, G: 30, B: 30, A: 255}
			}
			img.SetRGBA(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 92}); err != nil {
		t.Fatalf("encode fixture: %v", err)
	}
	base := buf.Bytes()

	upright, err := processAvatar(base)
	if err != nil {
		t.Fatalf("processAvatar: %v", err)
	}
	rotated, err := processAvatar(jpegWithOrientation(t, base, orientRotate90))
	if err != nil {
		t.Fatalf("processAvatar rotated: %v", err)
	}
	if bytes.Equal(upright, rotated) {
		t.Fatal("orientation tag was ignored: output is byte-identical to the unrotated image")
	}

	redAt := func(raw []byte, xf, yf float64) uint32 {
		t.Helper()
		decoded, err := jpeg.Decode(bytes.NewReader(raw))
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		b := decoded.Bounds()
		r, _, _, _ := decoded.At(b.Min.X+int(float64(b.Dx())*xf), b.Min.Y+int(float64(b.Dy())*yf)).RGBA()
		return r >> 8
	}

	// Upright: red band across the top.
	if top, bottom := redAt(upright, 0.5, 0.15), redAt(upright, 0.5, 0.85); top <= bottom {
		t.Errorf("upright: expected red on top, got top R=%d bottom R=%d", top, bottom)
	}
	// Rotated 90 clockwise: that band is now down the right-hand edge.
	if left, right := redAt(rotated, 0.15, 0.5), redAt(rotated, 0.85, 0.5); right <= left {
		t.Errorf("rotated: expected red on the right, got left R=%d right R=%d", left, right)
	}
}

func TestExifOrientationDefaultsToNormal(t *testing.T) {
	tests := []struct {
		name string
		body []byte
	}{
		{"no EXIF at all", solidJPEG(t, 200, 200, false)},
		{"not a JPEG", solidPNG(t, 200, 200, 255)},
		{"truncated", []byte{0xFF, 0xD8, 0xFF}},
		{"empty", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := exifOrientation(tc.body); got != orientNormal {
				t.Errorf("exifOrientation = %d, want %d", got, orientNormal)
			}
		})
	}
}

// A transparent PNG must not come out with black where it was see-through.
func TestProcessAvatarFlattensTransparencyToWhite(t *testing.T) {
	out, err := processAvatar(solidPNG(t, 300, 300, 0))
	if err != nil {
		t.Fatalf("processAvatar: %v", err)
	}
	img, err := jpeg.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	r, g, b, _ := img.At(img.Bounds().Dx()/2, img.Bounds().Dy()/2).RGBA()
	if r>>8 < 240 || g>>8 < 240 || b>>8 < 240 {
		t.Errorf("fully transparent pixel became rgb(%d,%d,%d), want near-white", r>>8, g>>8, b>>8)
	}
}

func TestValidAvatarKey(t *testing.T) {
	tests := []struct {
		key  string
		want bool
	}{
		{"AAAAAAAAAAAAAAAAAAAAAA", true},
		{"abc-DEF_012345678901ab", true},
		{"", false},
		{"tooshort", false},
		{"AAAAAAAAAAAAAAAAAAAAAAA", false}, // 23
		{"AAAAAAAAAAAAAAAAAAAAA", false},   // 21
		{"../../../etc/passwd12", false},
		{"AAAAAAAA/AAAAAAAAAAAA", false},
		{"AAAAAAAA.AAAAAAAAAAAA", false},
		{"AAAAAAAA%2eAAAAAAAAAA", false},
		{"AAAAAAAA AAAAAAAAAAAA", false},
	}
	for _, tc := range tests {
		if got := validAvatarKey(tc.key); got != tc.want {
			t.Errorf("validAvatarKey(%q) = %v, want %v", tc.key, got, tc.want)
		}
	}
}

func TestNewAvatarKeyIsUnguessableAndUnique(t *testing.T) {
	seen := make(map[string]bool, 200)
	for i := 0; i < 200; i++ {
		key, err := newAvatarKey()
		if err != nil {
			t.Fatalf("newAvatarKey: %v", err)
		}
		if !validAvatarKey(key) {
			t.Fatalf("newAvatarKey produced %q, which validAvatarKey rejects", key)
		}
		if seen[key] {
			t.Fatalf("newAvatarKey repeated %q", key)
		}
		seen[key] = true
	}
}

// jpegWithOrientation splices a minimal EXIF APP1 segment carrying the given
// orientation into an existing JPEG, immediately after the SOI marker.
func jpegWithOrientation(t *testing.T, src []byte, orientation int) []byte {
	t.Helper()

	// TIFF header (big endian) + one IFD entry for tag 0x0112.
	tiff := new(bytes.Buffer)
	tiff.WriteString("MM")
	_ = binary.Write(tiff, binary.BigEndian, uint16(42))
	_ = binary.Write(tiff, binary.BigEndian, uint32(8)) // IFD offset
	_ = binary.Write(tiff, binary.BigEndian, uint16(1)) // one entry
	_ = binary.Write(tiff, binary.BigEndian, uint16(exifOrientationID))
	_ = binary.Write(tiff, binary.BigEndian, uint16(3)) // SHORT
	_ = binary.Write(tiff, binary.BigEndian, uint32(1)) // count
	_ = binary.Write(tiff, binary.BigEndian, uint16(orientation))
	_ = binary.Write(tiff, binary.BigEndian, uint16(0)) // pad the value field
	_ = binary.Write(tiff, binary.BigEndian, uint32(0)) // next IFD

	payload := append([]byte("Exif\x00\x00"), tiff.Bytes()...)

	out := new(bytes.Buffer)
	out.Write(src[:2]) // SOI
	out.Write([]byte{0xFF, 0xE1})
	_ = binary.Write(out, binary.BigEndian, uint16(len(payload)+2))
	out.Write(payload)
	out.Write(src[2:])
	return out.Bytes()
}
