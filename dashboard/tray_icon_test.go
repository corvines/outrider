package main

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"testing"
)

func TestPaddedTrayIcon(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 36, 36))
	draw.Draw(source, source.Bounds(), image.NewUniform(color.Black), image.Point{}, draw.Src)
	var input bytes.Buffer
	if err := png.Encode(&input, source); err != nil {
		t.Fatal(err)
	}
	data, err := paddedTrayIcon(input.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	result, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if result.Bounds().Size() != image.Pt(60, 60) {
		t.Fatalf("bounds = %v", result.Bounds())
	}
	for y := 0; y < 60; y++ {
		for x := 0; x < 60; x++ {
			_, _, _, alpha := result.At(x, y).RGBA()
			inside := x >= 12 && x < 48 && y >= 12 && y < 48
			if (alpha != 0) != inside {
				t.Fatalf("unexpected alpha at %d,%d: %d", x, y, alpha)
			}
		}
	}
}

func TestPaddedTrayIconRejectsInvalidPNG(t *testing.T) {
	if _, err := paddedTrayIcon([]byte("invalid")); err == nil {
		t.Fatal("expected decode error")
	}
}
