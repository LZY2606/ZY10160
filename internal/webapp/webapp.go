// Package webapp embeds the single-page operation UI.
package webapp

import (
	"io/fs"
)

// Static returns the embedded UI filesystem (root contains index.html).
func Static() fs.FS {
	sub, err := fsSub()
	if err != nil {
		panic(err)
	}
	return sub
}
