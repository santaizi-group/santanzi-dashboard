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
	data, err := BarFormat("CPU", []BarItem{{Label: "a", Value: 10}}, AxisNumber, DarkPalette())
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

func TestFormatAxis(t *testing.T) {
	cases := []struct {
		v    float64
		axis AxisFormat
		want string
	}{
		{0, AxisNumber, "0.00"},
		{5, AxisNumber, "5.00"},
		{42.5, AxisNumber, "42.5"},
		{85.8, AxisNumber, "85.8"},
		{1234, AxisNumber, "1234"},
		{512, AxisBytes, "512B"},
		{2048, AxisBytes, "2.0KB"},
		{5.5e9, AxisBytes, "5.1GB"},
		{763 * 1024 * 1024, AxisBytes, "763MB"},
	}
	for _, tc := range cases {
		if got := formatAxis(tc.v, tc.axis); got != tc.want {
			t.Fatalf("formatAxis(%v, %d) = %q, want %q", tc.v, tc.axis, got, tc.want)
		}
	}
}
