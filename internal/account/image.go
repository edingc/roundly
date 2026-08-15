package account

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	_ "image/png" // registers the PNG decoder
	"net/http"
)

// Avatar processing bounds.
const (
	// MaxAvatarUploadBytes caps the request body. Deliberately below the 8-10 MB
	// a modern phone photo can reach: the global 30-second timeout in
	// internal/server covers this handler too, and a larger cap would turn a
	// slow mobile upload into a timeout rather than a clean rejection.
	MaxAvatarUploadBytes = 4 << 20

	// avatarSize is the stored edge length: twice the largest place the image
	// is rendered, so it stays sharp on a high-density display.
	avatarSize = 256

	// A source smaller than this is a favicon or a tracking pixel, not a
	// portrait, and would only ever be upscaled into mush.
	minSourceSide = 64
	// Guards against a decompression bomb: these are checked from the image
	// header alone, before any pixels are allocated.
	maxSourceSide   = 8000
	maxSourcePixels = 40_000_000

	avatarJPEGQuality = 85
	avatarContentType = "image/jpeg"
)

// errBadImage is a sentinel the handler turns into a per-field validation
// error, so the message lands on the file input rather than as a page error.
type errBadImage struct{ msg string }

func (e errBadImage) Error() string { return e.msg }

// processAvatar turns an uploaded image into the square JPEG that gets stored.
//
// The re-encode is what strips metadata. jpeg.Encode writes from decoded pixels
// and has no path for EXIF at all, so the GPS coordinates, camera serial, and
// embedded thumbnail that a phone attaches cannot survive it. That is a
// privacy guarantee of the pipeline, not a side effect worth relying on
// accidentally — hence this comment rather than none.
func processAvatar(raw []byte) ([]byte, error) {
	// Sniff the bytes rather than trusting the multipart part's Content-Type,
	// which is entirely attacker-controlled.
	sniffed := http.DetectContentType(raw)
	switch sniffed {
	case "image/jpeg", "image/png":
	default:
		return nil, errBadImage{"Upload a JPEG or PNG image."}
	}

	// Header-only check first: this rejects a bomb before decoding allocates
	// anything proportional to the claimed dimensions.
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return nil, errBadImage{"That image could not be read."}
	}
	if cfg.Width < minSourceSide || cfg.Height < minSourceSide {
		return nil, errBadImage{fmt.Sprintf("That image is too small — use one at least %d pixels on each side.", minSourceSide)}
	}
	if cfg.Width > maxSourceSide || cfg.Height > maxSourceSide || cfg.Width*cfg.Height > maxSourcePixels {
		return nil, errBadImage{"That image is too large to process."}
	}

	src, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, errBadImage{"That image could not be read."}
	}

	// JPEG decoding ignores the EXIF orientation tag, so a photo taken in
	// portrait on a phone arrives on its side unless this is applied.
	if sniffed == "image/jpeg" {
		src = applyOrientation(src, exifOrientation(raw))
	}

	square := cropSquare(src)
	size := square.Bounds().Dx()
	if size > avatarSize {
		size = avatarSize
	}
	// Never upscale: enlarging a small source produces a blurry image that
	// merely takes up more space.
	scaled := resize(square, size)

	// Composite onto white before encoding. JPEG has no alpha channel, so a
	// transparent PNG would otherwise come out with black wherever it was
	// see-through.
	out := image.NewRGBA(image.Rect(0, 0, size, size))
	draw.Draw(out, out.Bounds(), image.White, image.Point{}, draw.Src)
	draw.Draw(out, out.Bounds(), scaled, scaled.Bounds().Min, draw.Over)

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, out, &jpeg.Options{Quality: avatarJPEGQuality}); err != nil {
		return nil, fmt.Errorf("encode avatar: %w", err)
	}
	return buf.Bytes(), nil
}

// cropSquare takes the largest centered square from an image.
func cropSquare(src image.Image) image.Image {
	b := src.Bounds()
	side := b.Dx()
	if b.Dy() < side {
		side = b.Dy()
	}
	// Centered rather than top-left: a portrait's subject is far more often in
	// the middle than in a corner.
	x0 := b.Min.X + (b.Dx()-side)/2
	y0 := b.Min.Y + (b.Dy()-side)/2
	rect := image.Rect(x0, y0, x0+side, y0+side)

	if sub, ok := src.(interface {
		SubImage(image.Rectangle) image.Image
	}); ok {
		return sub.SubImage(rect)
	}

	out := image.NewRGBA(image.Rect(0, 0, side, side))
	draw.Draw(out, out.Bounds(), src, rect.Min, draw.Src)
	return out
}

// resize scales a square image down to size×size by box filtering: each
// destination pixel is the average of the source pixels it covers.
//
// A box filter is the right choice for pure reduction — it uses every source
// pixel exactly once, so it neither aliases the way nearest-neighbour does nor
// needs the extra sampling of a bicubic kernel at these ratios. This is why
// there is no image-scaling dependency here.
func resize(src image.Image, size int) image.Image {
	b := src.Bounds()
	side := b.Dx()
	if side == size {
		out := image.NewRGBA(image.Rect(0, 0, size, size))
		draw.Draw(out, out.Bounds(), src, b.Min, draw.Src)
		return out
	}

	out := image.NewRGBA(image.Rect(0, 0, size, size))
	for dy := 0; dy < size; dy++ {
		sy0 := b.Min.Y + dy*side/size
		sy1 := b.Min.Y + (dy+1)*side/size
		if sy1 <= sy0 {
			sy1 = sy0 + 1
		}
		for dx := 0; dx < size; dx++ {
			sx0 := b.Min.X + dx*side/size
			sx1 := b.Min.X + (dx+1)*side/size
			if sx1 <= sx0 {
				sx1 = sx0 + 1
			}

			var sr, sg, sb, sa, n uint64
			for sy := sy0; sy < sy1; sy++ {
				for sx := sx0; sx < sx1; sx++ {
					r, g, bl, a := src.At(sx, sy).RGBA()
					sr += uint64(r)
					sg += uint64(g)
					sb += uint64(bl)
					sa += uint64(a)
					n++
				}
			}
			if n == 0 {
				continue
			}
			// RGBA() returns 16-bit values; shift back down to 8.
			out.SetRGBA(dx, dy, colorFrom(sr/n, sg/n, sb/n, sa/n))
		}
	}
	return out
}
