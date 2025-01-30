package main

import (
	"context"
	"crypto/tls"
	"net/http"

	"github.com/slcjordan/autodemo/client"
	"github.com/slcjordan/autodemo/logger"
	"github.com/slcjordan/autodemo/pki"
	"github.com/slcjordan/autodemo/proxy"
	"github.com/slcjordan/autodemo/transport"
)

var insecureTransport = &http.Transport{
	TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
}

/*
type debugTransport struct{}

func (d *debugTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var curr autodemo.RoundTripDump
	var err error

	curr.Request, err = httputil.DumpRequestOut(req, true)
	if err != nil {
		logger.Errorf(req.Context(), "could not dump request for %s %q: %s", req.Method, req.URL.String(), err)
	}
	resp, err := insecureTransport.RoundTrip(req)
	if err != nil {
		logger.Errorf(req.Context(), "could not dump response for %s %q: %s", req.Method, req.URL.String(), err)
	}
	curr.Response, err = httputil.DumpResponse(resp, true)

	fmt.Println(string(curr.Request))
	fmt.Println(string(curr.Response))
	return resp, err
}
*/

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	caKeyfile := "ca_key.pem"
	caCertfile := "ca_cert.pem"
	eeKeyfile := "ee_key.pem"

	pkiProvider, err := pki.NewProvider(eeKeyfile, caKeyfile, caCertfile)
	if err != nil {
		panic(err)
	}
	workerClient := &client.Worker{
		Addr: "localhost:8080",
	}
	insecureCurl := &transport.Curl{
		Transport: insecureTransport,
		Listener:  workerClient,
		Insecure:  true,
	}
	secureCurl := &transport.Curl{
		Transport: http.DefaultTransport,
		Listener:  workerClient,
	}
	workerClient.Reset = func() {
		insecureCurl.Reset()
		secureCurl.Reset()
	}
	manager := proxy.NewManager(secureCurl, insecureCurl, pkiProvider, workerClient)

	defer manager.Shutdown(ctx)
	defer workerClient.StopProject(ctx, "Verify digest escrow signing works")

	http.ListenAndServe("0.0.0.0:11080", logger.Middleware(manager))
}
