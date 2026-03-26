package proxy

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"

	"github.com/elazarl/goproxy"
)

// Proxy manages the shared CA and stream registry.
type Proxy struct {
	CA       *CA
	Registry *StreamRegistry
}

// NewProxy creates the shared proxy infrastructure (CA + registry).
func NewProxy(configDir string) (*Proxy, error) {
	ca, err := EnsureCA(configDir)
	if err != nil {
		return nil, fmt.Errorf("ensure CA: %w", err)
	}

	return &Proxy{
		CA:       ca,
		Registry: NewStreamRegistry(),
	}, nil
}

// StartTerminalProxy creates and starts a per-terminal MITM proxy listener.
// bindHost should be "127.0.0.1" for regular terminals or "0.0.0.0" for sandboxed.
func (p *Proxy) StartTerminalProxy(terminalID, bindHost string) (*TerminalProxy, error) {
	gp := goproxy.NewProxyHttpServer()
	gp.Verbose = false

	// Set up MITM for api.anthropic.com only.
	x509Cert, err := x509.ParseCertificate(p.CA.Certificate.Certificate[0])
	if err != nil {
		return nil, fmt.Errorf("parse CA cert: %w", err)
	}

	tlsConfig := func(host string, ctx *goproxy.ProxyCtx) (*tls.Config, error) {
		return goproxy.TLSConfigFromCA(&p.CA.Certificate)(host, ctx)
	}

	gp.OnRequest(goproxy.ReqHostIs("api.anthropic.com:443")).HandleConnect(
		goproxy.AlwaysMitm,
	)

	// For non-Anthropic hosts, just tunnel through.
	gp.OnRequest().HandleConnectFunc(func(host string, ctx *goproxy.ProxyCtx) (*goproxy.ConnectAction, string) {
		if host == "api.anthropic.com:443" {
			return &goproxy.ConnectAction{
				Action:    goproxy.ConnectMitm,
				TLSConfig: tlsConfig,
			}, host
		}
		return goproxy.OkConnect, host
	})

	// Set the CA for goproxy's MITM cert generation.
	goproxy.GoproxyCa = p.CA.Certificate
	_ = x509Cert // used above via CA.Certificate

	// Intercept responses from POST /v1/messages to parse SSE.
	gp.OnResponse().DoFunc(func(resp *http.Response, ctx *goproxy.ProxyCtx) *http.Response {
		if resp == nil || ctx.Req == nil {
			return resp
		}

		// Only intercept streaming responses to /v1/messages.
		if ctx.Req.Method != "POST" || !strings.HasSuffix(ctx.Req.URL.Path, "/v1/messages") {
			return resp
		}

		contentType := resp.Header.Get("Content-Type")
		if !strings.Contains(contentType, "text/event-stream") {
			return resp
		}

		streamID, isSubagent := p.Registry.StartStream(terminalID)

		// Replace the response body with a tee that feeds our SSE parser.
		pr, pw := io.Pipe()
		originalBody := resp.Body

		resp.Body = &teeReadCloser{
			reader: io.TeeReader(originalBody, pw),
			closer: func() error {
				pw.Close()
				return originalBody.Close()
			},
		}

		// Parse SSE in background.
		go func() {
			defer p.Registry.EndStream(terminalID)
			ParseAgentEvents(pr, streamID, terminalID, isSubagent, p.Registry.Events)
		}()

		return resp
	})

	// Listen on dynamic port.
	ln, err := net.Listen("tcp", bindHost+":0")
	if err != nil {
		return nil, fmt.Errorf("listen: %w", err)
	}

	addr := ln.Addr().(*net.TCPAddr)
	tp := &TerminalProxy{
		TerminalID: terminalID,
		Addr:       fmt.Sprintf("%s:%d", bindHost, addr.Port),
		Port:       addr.Port,
		registry:   p.Registry,
	}

	go func() {
		srv := &http.Server{Handler: gp}
		if err := srv.Serve(ln); err != nil && !tp.stop.Load() {
			log.Printf("proxy for terminal %s: %v", terminalID, err)
		}
	}()

	return tp, nil
}

// teeReadCloser wraps a TeeReader with a custom close function.
type teeReadCloser struct {
	reader io.Reader
	closer func() error
}

func (t *teeReadCloser) Read(p []byte) (int, error) {
	return t.reader.Read(p)
}

func (t *teeReadCloser) Close() error {
	return t.closer()
}
