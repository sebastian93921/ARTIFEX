//go:build !embedui

package server

import (
	"github.com/sebastian93921/artifex/locale"
	"net/http"
)

// webuiHandler is the no-embed stub (default build). The frontend is NOT bundled
// into this binary — run it separately with `cd web && npm run dev` during
// development. Build the bundled single-binary with:
//
//	(cd web && npm run build:static)   # produces web/out; stay at repo root
//	mkdir -p server/webui/dist
//	cp -R web/out/. server/webui/dist/  # (or use the build script)
//	go build -tags embedui ./cmd/artifex
func (s *Server) webuiHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, locale.Text(locale.FromRequest(r), "The frontend is not embedded in this binary (use next dev for development; build releases with -tags embedui)"), http.StatusNotFound)
	})
}
