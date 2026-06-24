package server

import (
	"net/http"
)

func (s *Server) absoluteURL(r *http.Request, path string) string {
	if s.publicBaseURL != "" {
		return s.publicBaseURL + path
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if forwarded := r.Header.Get("X-Forwarded-Proto"); forwarded != "" {
		scheme = forwarded
	}
	return scheme + "://" + r.Host + path
}
