package agentintegration

import (
	"embed"
	"io/fs"
)

//go:embed assets/wktbox-isolated-development
var embeddedAssets embed.FS

func BundledAssets() fs.FS {
	assets, err := fs.Sub(embeddedAssets, "assets")
	if err != nil {
		panic(err)
	}
	return assets
}
