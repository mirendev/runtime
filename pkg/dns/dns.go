package dns

import (
	"fmt"
	"net"

	"github.com/miekg/dns"
)

type Server struct {
	*dns.Server
	client    *dns.Client
	upstreams []string
}

// New creates a new DNS forwarding server
func New(addr string) (*Server, error) {
	cc, err := dns.ClientConfigFromFile("/etc/resolv.conf")
	if err != nil {
		return nil, fmt.Errorf("reading resolv.conf: %w", err)
	}

	upstreams := cc.Servers

	if len(upstreams) == 0 {
		return nil, fmt.Errorf("no nameservers found in /etc/resolv.conf")
	}

	s := &Server{
		Server: &dns.Server{
			Addr: addr,
			Net:  "udp",
		},
		client:    &dns.Client{},
		upstreams: upstreams,
	}

	s.Handler = dns.HandlerFunc(s.handleRequest)
	return s, nil
}

func (s *Server) handleRequest(w dns.ResponseWriter, r *dns.Msg) {
	// Try each upstream server until we get a response
	var response *dns.Msg
	var err error

	for _, upstream := range s.upstreams {
		// Ensure upstream address has port 53
		upstream = net.JoinHostPort(upstream, "53")
		response, _, err = s.client.Exchange(r, upstream)
		if err == nil && response != nil {
			break
		}
	}

	if err != nil || response == nil {
		// If all upstreams failed, return SERVFAIL
		response = new(dns.Msg)
		response.SetReply(r)
		response.Rcode = dns.RcodeServerFailure
		w.WriteMsg(response)
		return
	}

	// Ensure the response has the correct id and is marked as a response
	response.Id = r.Id
	response.RecursionAvailable = true
	response.Response = true

	w.WriteMsg(response)
}

// ListenAndServe starts the DNS server
func (s *Server) ListenAndServe() error {
	return s.Server.ListenAndServe()
}

// Shutdown gracefully shuts down the server
func (s *Server) Shutdown() error {
	return s.Server.Shutdown()
}
