package server

import (
	clientapp "laz/internal/server/transport/http/client"

	"github.com/go-chi/chi/v5"
)

type ClientApp struct {
	app *clientapp.App
}

func (s *Server) ClientApp() *ClientApp {
	return &ClientApp{
		app: clientapp.New(clientapp.Config{
			Store:            s.store,
			ClientAuth:       s.clientAuth,
			Subscriptions:    s.subscriptions,
			GetOrCreateToken: s.clientTokens.GetOrCreate,
			AbsoluteURL:      s.absoluteURL,
			ConnectAssets:    s.connectAssets(),
		}),
	}
}

func (c *ClientApp) Mount(router chi.Router) {
	c.app.Mount(router)
}
