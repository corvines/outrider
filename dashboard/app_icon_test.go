package main

import (
	"bytes"
	"image"
	"image/png"
	"testing"
)

func TestAppIconHasTransparentMargin(t *testing.T) {
	icon, err := png.Decode(bytes.NewReader(appIcon))
	if err != nil {
		t.Fatal(err)
	}
	bounds := icon.Bounds()
	if bounds.Dx() != bounds.Dy() {
		t.Fatalf("app icon is not square: %v", bounds)
	}
	margin := bounds.Dx() / 10
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			_, _, _, alpha := icon.At(x, y).RGBA()
			outside := x < bounds.Min.X+margin || x >= bounds.Max.X-margin || y < bounds.Min.Y+margin || y >= bounds.Max.Y-margin
			if outside && alpha != 0 {
				t.Fatalf("expected transparent margin at %d,%d", x, y)
			}
		}
	}
	_, _, _, alpha := icon.At(bounds.Min.X+bounds.Dx()/2, bounds.Min.Y+bounds.Dy()/2).RGBA()
	if alpha == 0 {
		t.Fatal("app icon has no visible center")
	}
}

func TestAppIconHasRoundedCorners(t *testing.T) {
	icon, err := png.Decode(bytes.NewReader(appIcon))
	if err != nil {
		t.Fatal(err)
	}
	bounds := icon.Bounds()
	inset := bounds.Dx()/10 + bounds.Dx()/50
	for _, point := range []image.Point{
		{bounds.Min.X + inset, bounds.Min.Y + inset},
		{bounds.Max.X - inset - 1, bounds.Min.Y + inset},
		{bounds.Min.X + inset, bounds.Max.Y - inset - 1},
		{bounds.Max.X - inset - 1, bounds.Max.Y - inset - 1},
	} {
		_, _, _, alpha := icon.At(point.X, point.Y).RGBA()
		if alpha != 0 {
			t.Fatalf("expected rounded corner to be transparent at %v", point)
		}
	}
	for _, point := range []image.Point{
		{bounds.Min.X + bounds.Dx()/2, bounds.Min.Y + inset},
		{bounds.Min.X + bounds.Dx()/2, bounds.Max.Y - inset - 1},
		{bounds.Min.X + inset, bounds.Min.Y + bounds.Dy()/2},
		{bounds.Max.X - inset - 1, bounds.Min.Y + bounds.Dy()/2},
	} {
		_, _, _, alpha := icon.At(point.X, point.Y).RGBA()
		if alpha != 0xffff {
			t.Fatalf("expected tile edge to remain opaque at %v", point)
		}
	}
}
