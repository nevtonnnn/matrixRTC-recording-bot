package layouts

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed screen.html
var layoutFiles embed.FS

func Handler(prefix string) http.Handler {
	sub, _ := fs.Sub(layoutFiles, ".")
	return http.StripPrefix(prefix, http.FileServer(http.FS(sub)))
}
