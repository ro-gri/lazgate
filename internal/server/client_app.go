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
			Store:                        s.store,
			ClientSelfService:            s.clientSelfService,
			Events:                       s.events,
			Subscriptions:                s.subscriptions,
			GetOrCreateSubscriptionToken: s.clientTokens.GetOrCreateSubscription,
			AbsoluteURL:                  s.absoluteURL,
			ConnectAssets:                s.connectAssets(),
			Secret:                       s.secretKey,
			RateLimits:                   s.rateLimits,
		}),
	}
}

func (c *ClientApp) Mount(router chi.Router) {
	c.app.Mount(router)
}
