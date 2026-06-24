package server

import "laz/internal/model"

func (s *Server) listConfigProfiles() []model.ConfigProfile {
	if s.store != nil {
		return s.store.ListConfigProfiles()
	}
	return []model.ConfigProfile{}
}
