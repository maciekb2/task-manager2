package lab

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed web/*
var assets embed.FS

func staticHandler() http.Handler {
	root, _ := fs.Sub(assets, "web")
	return http.FileServer(http.FS(root))
}
