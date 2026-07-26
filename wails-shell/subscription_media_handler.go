package main

import (
	"net/http"
	"path/filepath"
)

const subscriptionMediaRoute = "/subscription-media/"

// subscriptionMediaHandler exposes only the app-managed actor media directory
// through Wails' internal asset server. WebView2 blocks file:// URLs loaded by
// the application page, while same-origin asset URLs render normally.
func (a *App) subscriptionMediaHandler() http.Handler {
	mediaRoot := filepath.Join(a.paths.UserData, "subscriptions-v2", "media")
	mux := http.NewServeMux()
	mux.Handle(subscriptionMediaRoute, http.StripPrefix(
		subscriptionMediaRoute,
		http.FileServer(http.Dir(mediaRoot)),
	))
	return mux
}
