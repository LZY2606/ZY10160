package webapp

import (
	"embed"
	"io/fs"
)

//go:embed static/index.html static/app.js static/styles.css
var files embed.FS

func fsSub() (fs.FS, error) {
	return fs.Sub(files, "static")
}
