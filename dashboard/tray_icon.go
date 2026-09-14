package main

import (
	"bytes"
	"image"
	"image/draw"
	"image/png"
)

// The status bar renderer scales the whole canvas to the menu bar height.
func paddedTrayIcon(data []byte) ([]byte, error) {
	source, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	bounds := source.Bounds()
	side := (max(bounds.Dx(), bounds.Dy())*5 + 2) / 3
	canvas := image.NewNRGBA(image.Rect(0, 0, side, side))
	offset := image.Pt((side-bounds.Dx())/2, (side-bounds.Dy())/2)
	draw.Draw(canvas, image.Rectangle{Min: offset, Max: offset.Add(bounds.Size())}, source, bounds.Min, draw.Src)
	var output bytes.Buffer
	if err := png.Encode(&output, canvas); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}
