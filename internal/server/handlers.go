package server

import "net/http"

func (s *Server) registerHandlers() {
	router := s.router

	router.HandleFunc("POST /token", s.postToken)
	router.HandleFunc("DELETE /token", s.deleteToken)
}

func (s *Server) postToken(w http.ResponseWriter, r *http.Request) {
	// TODO: implement this
}

func (s *Server) deleteToken(w http.ResponseWriter, r *http.Request) {
	// TODO: implement this
}
