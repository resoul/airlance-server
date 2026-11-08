package webfetch

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
)

const (
	maxSide     = 8000
	jpegQuality = 82
)

func (c *Client) image(ctx context.Context, link string) ([]byte, int32, int32) {
	body, _, _, err := c.get(ctx, link, "image/jpeg,image/png,image/gif", maxImage)
	if err != nil {
		return nil, 0, 0
	}

	config, format, err := image.DecodeConfig(bytes.NewReader(body))
	if err != nil || config.Width <= 0 || config.Height <= 0 || config.Width > maxSide || config.Height > maxSide {
		return nil, 0, 0
	}

	if format == "jpeg" {
		return body, int32(config.Width), int32(config.Height)
	}

	decoded, _, err := image.Decode(bytes.NewReader(body))
	if err != nil {
		return nil, 0, 0
	}

	flat := image.NewRGBA(decoded.Bounds())
	draw.Draw(flat, flat.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.Draw(flat, flat.Bounds(), decoded, decoded.Bounds().Min, draw.Over)

	var out bytes.Buffer
	if err := jpeg.Encode(&out, flat, &jpeg.Options{Quality: jpegQuality}); err != nil {
		return nil, 0, 0
	}

	return out.Bytes(), int32(config.Width), int32(config.Height)
}
