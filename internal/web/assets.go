package web

import "embed"

// Assets contains the embedded operation page.
//
//go:embed all:webroot
var Assets embed.FS
