package main

import (
	"embed"
	"fmt"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	// Create an instance of the app structure
	app := NewApp()

	// Create application with options
	err := wails.Run(&options.App{
		Title:  fmt.Sprintf("%s v%s", appDisplayName, AppVersion),
		Width:  1054,
		Height: 768,
		// The window is otherwise freely resizable (and correctly per-monitor
		// DPI-aware - see build/windows/wails.exe.manifest's dpiAwareness
		// declaration, which WebView2 honors automatically) - this floor just
		// keeps the user from shrinking it below a size the flex-wrap layout
		// has actually been verified to still hold up at with no clipping or
		// overlap.
		MinWidth:  700,
		MinHeight: 520,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 27, G: 38, B: 54, A: 1},
		OnStartup:        app.startup,
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}
