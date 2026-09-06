package media

import (
	"image"
	"image/jpeg"
	"io"

	xdraw "golang.org/x/image/draw"
)

// scaleToMaxEdge 等比缩放到最长边 maxEdge；小于 maxEdge 的图不放大。
func scaleToMaxEdge(src image.Image, maxEdge int) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= maxEdge && h <= maxEdge {
		return src
	}
	scale := float64(maxEdge) / float64(max(w, h))
	dw := max(1, int(float64(w)*scale+0.5))
	dh := max(1, int(float64(h)*scale+0.5))
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	xdraw.ApproxBiLinear.Scale(dst, dst.Rect, src, b, xdraw.Over, nil)
	return dst
}

func encodeJPEG(w io.Writer, img image.Image) error {
	return jpeg.Encode(w, img, &jpeg.Options{Quality: 85})
}
