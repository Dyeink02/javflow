package main

import (
	"net/http"
	"path/filepath"
)

const subscriptionMediaRoute = "/subscription-media/"

// subscriptionMediaHandler serves only actor media cached by the application.
// Keeping this route separate prevents provider hotlink failures in WebView2
// without exposing arbitrary local paths or the crawler output tree.
func (a *App) subscriptionMediaHandler() http.Handler {
	mediaRoot := filepath.Join(a.paths.UserData, "subscriptions-v2", "media")
	mux := http.NewServeMux()
	mux.Handle(subscriptionMediaRoute, http.StripPrefix(
		subscriptionMediaRoute,
		http.FileServer(http.Dir(mediaRoot)),
	))
	return mux
}
