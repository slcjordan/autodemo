package dns

import (
	"context"
	"fmt"
	"net"
	"strings"

	"github.com/miekg/dns"
	"github.com/slcjordan/manualqa/logger"
)

func handleDNSQuery(w dns.ResponseWriter, req *dns.Msg) {
	ctx := context.Background()
	m := new(dns.Msg)
	m.SetReply(req)
	m.Authoritative = false

	for _, q := range req.Question {
		hostname := strings.TrimSuffix(q.Name, ".")

		// Resolve hostname using system DNS
		ips, err := net.LookupHost(hostname)
		if err != nil {
			logger.Infof(ctx, "Failed to resolve %s: %v\n", hostname, err)
			continue
		}

		// Check if the resolution points to 127.0.0.1
		useDockerInternal := false
		for _, ip := range ips {
			if ip == "127.0.0.1" {
				useDockerInternal = true
				break
			}
		}

		// If localhost, return "host.docker.internal"
		var resolvedIP string
		if useDockerInternal {
			internalIPs, err := net.LookupHost("host.docker.internal")
			if err != nil {
				logger.Infof(ctx, "Failed to resolve %s: %v\n", hostname, err)
				continue
			}
			resolvedIP = internalIPs[0] // Use the first resolved IP
		} else {
			resolvedIP = ips[0] // Use the first resolved IP
		}

		// Create a DNS response
		rr, _ := dns.NewRR(fmt.Sprintf("%s A %s", q.Name, resolvedIP))
		m.Answer = append(m.Answer, rr)
	}

	w.WriteMsg(m)
}

func RunDockerResolver(ctx context.Context, port uint16) error {

	dns.HandleFunc(".", handleDNSQuery)
	server := &dns.Server{Addr: fmt.Sprintf(":%d", port), Net: "udp"}

	logger.Infof(ctx, "Starting custom DNS server on UDP :%d...", port)
	return server.ListenAndServe()
}
