package chart

import (
	"bytes"
	"image/png"
	"testing"
)

func TestBarPNG(t *testing.T) {
	data, err := Bar("CPU", []BarItem{{Label: "a", Value: 10}, {Label: "b", Value: 20}})
	if err != nil {
		t.Fatal(err)
	}
	if len(data) < 32 || !bytes.HasPrefix(data, []byte{137, 80, 78, 71}) {
		t.Fatalf("not png: %d", len(data))
	}
	if _, err := png.Decode(bytes.NewReader(data)); err != nil {
		t.Fatal(err)
	}
}

func TestDarkBarPNG(t *testing.T) {
	data, err := BarTheme("CPU", []BarItem{{Label: "a", Value: 10}}, DarkPalette())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := png.Decode(bytes.NewReader(data)); err != nil {
		t.Fatal(err)
	}
}

func TestLinePNG(t *testing.T) {
	data, err := Line("load", []Series{{Name: "cpu", Points: []float64{1, 3, 2, 5}}}, []string{"0", "3"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := png.Decode(bytes.NewReader(data)); err != nil {
		t.Fatal(err)
	}
}
