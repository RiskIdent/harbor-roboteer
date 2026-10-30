package server

import (
	"log"
	"net"
	"net/http"
)

type Server struct {
	router   *http.ServeMux
	server   *http.Server
	listener net.Listener
}

func NewServer(port string) *Server {
	r := http.NewServeMux()
	l, err := net.Listen("tcp", ":"+port)
	if err != nil {
		log.Fatalf("couldn't create the listener: %v", err)
	}
	s := &Server{
		router: r,
		server: &http.Server{
			Handler: r,
		},
		listener: l,
	}
	s.registerHandlers()
	return s
}

func (s *Server) Start() error {
	return s.server.Serve(s.listener)
}
