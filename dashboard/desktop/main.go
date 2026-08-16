package main

import (
	"embed"
	"flag"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
)

//go:embed all:frontend/dist
var assets embed.FS

// defaultRepoPath is baked in at build time via -ldflags "-X main.defaultRepoPath=...".
// When empty, the app falls back to the current working directory.
var defaultRepoPath = ""

func main() {
	pathFlag := flag.String("path", defaultRepoPath, "Path to the career-ops repo")
	flag.Parse()

	app := NewApp(resolveRepoPath(*pathFlag))

	err := wails.Run(&options.App{
		Title:  "Career Dashboard",
		Width:  1180,
		Height: 780,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		OnStartup: app.startup,
		Bind:      []interface{}{app},
		Mac: &mac.Options{
			TitleBar: mac.TitleBarHiddenInset(),
			About: &mac.AboutInfo{
				Title:   "Career Dashboard",
				Message: "career-ops job search dashboard",
			},
		},
	})
	if err != nil {
		panic(err)
	}
}
