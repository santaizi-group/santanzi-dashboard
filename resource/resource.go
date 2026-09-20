package resource

import (
	"embed"

	"github.com/hi2shark/santaizi-dashboard/pkg/utils"
)

var StaticFS *utils.HybridFS

//go:embed static/logo.svg static/brand.svg static/app-icon.svg static/manifest-192x192.png static/manifest-512x512.png static/manifest-*.json static/theme-server-status/img static/theme-nazhua/maps
var staticFS embed.FS

//go:embed web
var WebFS embed.FS

//go:embed l10n
var I18nFS embed.FS

func init() {
	var err error
	StaticFS, err = utils.NewHybridFS(staticFS, "static", "resource/static/custom")
	if err != nil {
		panic(err)
	}
}
