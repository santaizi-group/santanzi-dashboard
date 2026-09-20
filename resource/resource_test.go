package resource

import (
	"bytes"
	"encoding/xml"
	"image"
	_ "image/png"
	"strings"
	"testing"
)

func TestSantaiziBrandAssets(t *testing.T) {
	for _, name := range []string{"logo.svg", "brand.svg", "app-icon.svg"} {
		content, err := staticFS.ReadFile("static/" + name)
		if err != nil {
			t.Fatal(err)
		}
		var root struct {
			XMLName xml.Name
		}
		if err := xml.Unmarshal(content, &root); err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		if root.XMLName.Local != "svg" {
			t.Fatalf("%s root is %q, want svg", name, root.XMLName.Local)
		}
		upper := strings.ToUpper(string(content))
		if strings.Contains(upper, "NEZHA") {
			t.Fatalf("%s contains the old product brand", name)
		}
	}

	brand, err := staticFS.ReadFile("static/brand.svg")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(brand), "三太子监控") || !strings.Contains(string(brand), "SANTAIZI MONITORING") {
		t.Fatal("brand.svg must contain the Chinese and English Santaizi brand names")
	}

	for _, name := range []string{
		"theme-nazhua/maps/world.geo.json",
		"theme-server-status/img/bg.jpg",
	} {
		if _, err := staticFS.ReadFile("static/" + name); err != nil {
			t.Fatalf("embedded theme asset %s: %v", name, err)
		}
	}

	for name, want := range map[string]int{"manifest-192x192.png": 192, "manifest-512x512.png": 512} {
		content, err := staticFS.ReadFile("static/" + name)
		if err != nil {
			t.Fatal(err)
		}
		config, _, err := image.DecodeConfig(bytes.NewReader(content))
		if err != nil {
			t.Fatalf("decode %s: %v", name, err)
		}
		if config.Width != want || config.Height != want {
			t.Fatalf("%s is %dx%d, want %dx%d", name, config.Width, config.Height, want, want)
		}
	}
}
