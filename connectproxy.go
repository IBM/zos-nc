package main

// Local vendored copy of the runtime code from
// github.com/wrouesnel/go.connect-proxy-scheme
// (itself a refactor of github.com/mwitkow/go-http-dialer to be
// compatible with golang.org/x/net/proxy scheme registration).
//
// Only the runtime files (auth.go, dialer.go) are vendored here. The
// upstream module pulls in build-time-only dependencies (notably
// github.com/mholt/archiver via its magefile.go with `//go:build mage`
// tag, and transitively github.com/nwaples/rardecode) which are flagged
// by Dependabot (CVE-2025-3445, CVE-2024-0406, CVE-2019-10743,
// CVE-2025-11579) even though they are never imported by built packages
// (`go list -deps ./...` does not include them) and `go mod tidy` does
// not prune them from go.mod. Vendoring removes the parent dependency
// entirely and thus remediates all four alerts, as the deprecated
// mholt/archiver v3 line has no patched release (successor is
// github.com/mholt/archives) and rardecode v1 has no fix (fixed only in
// rardecode/v2 v2.2.0, which archiver v3 does not use).
//
// Original code is licensed under the Apache License 2.0.
// See https://github.com/wrouesnel/go.connect-proxy-scheme for upstream.

import (
	"bufio"
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/net/proxy"
)

const (
	hdrProxyAuthResp = "Proxy-Authorization"
	hdrProxyAuthReq  = "Proxy-Authenticate"
)

// ProxyAuthorization allows for plugging in arbitrary implementations of the "Proxy-Authorization" handler.
type ProxyAuthorization interface {
	// Type represents what kind of Authorization, e.g. "Bearer", "Token", "Digest".
	Type() string

	// Initial allows you to specify an a-priori "Proxy-Authenticate" response header, attached to first request,
	// so you don't need to wait for an additional challenge. If empty string is returned, "Proxy-Authenticate"
	// header is added.
	InitialResponse() string

	// ChallengeResponse returns the content of the "Proxy-Authenticate" response header, that has been chose as
	// response to "Proxy-Authorization" request header challenge.
	ChallengeResponse(challenge string) string
}

type basicAuth struct {
	username string
	password string
}

// AuthBasic returns a ProxyAuthorization that implements "Basic" protocol while ignoring realm challenges.
func AuthBasic(username string, password string) ProxyAuthorization {
	return &basicAuth{username: username, password: password}
}

func (b *basicAuth) Type() string {
	return "Basic"
}

func (b *basicAuth) InitialResponse() string {
	return b.authString()
}

func (b *basicAuth) ChallengeResponse(challenge string) string {
	// challenge can be realm="proxy.com"
	// TODO(mwitkow): Implement realm lookup in AuthBasicWithRealm.
	return b.authString()
}

func (b *basicAuth) authString() string {
	resp := b.username + ":" + b.password
	return base64.StdEncoding.EncodeToString([]byte(resp))
}

// New constructs an HttpConnectTunnel to be used a net.Dial command.
// The first parameter is a proxy URL, for example https://foo.example.com:9090 will use foo.example.com as proxy on
// port 9090 using TLS for connectivity.
func New(proxyURL *url.URL, dialer proxy.Dialer) (*HttpConnectTunnel, error) {
	t := &HttpConnectTunnel{
		parentDialer: dialer,
		proxyScheme:  proxyURL.Scheme,
		proxyHost:    proxyURL.Hostname(),
		proxyPort:    proxyURL.Port(),
		proxyPath:    proxyURL.Path,
	}

	if t.proxyPort == "" {
		if t.proxyScheme == "https" {
			t.proxyPort = "443"
		} else {
			t.proxyPort = "8080"
		}
	}

	return t, nil
}

// ConnectProxy registers as a proxy.Dialer constructor for the "http" scheme
// via proxy.RegisterDialerType.
func ConnectProxy(proxyURL *url.URL, dialer proxy.Dialer) (proxy.Dialer, error) {
	return New(proxyURL, dialer)
}

var _ = proxy.Dialer(HttpConnectTunnel{})
var _ = proxy.ContextDialer(HttpConnectTunnel{})

// HttpConnectTunnel represents a configured HTTP Connect Tunnel dialer.
type HttpConnectTunnel struct {
	parentDialer proxy.Dialer
	proxyScheme  string
	proxyHost    string
	proxyPort    string
	proxyPath    string
	auth         ProxyAuthorization
}

func (t HttpConnectTunnel) dialProxy(ctx context.Context) (net.Conn, error) {
	// TODO: TLS proxy support
	if f, ok := t.parentDialer.(proxy.ContextDialer); ok {
		return f.DialContext(ctx, "tcp", net.JoinHostPort(t.proxyHost, t.proxyPort))
	}
	return dialContext(ctx, t.parentDialer, "tcp", net.JoinHostPort(t.proxyHost, t.proxyPort))
}

func (t HttpConnectTunnel) DialContext(ctx context.Context, network string, address string) (net.Conn, error) {
	if network != "tcp" {
		return nil, fmt.Errorf("network type '%v' unsupported (only 'tcp')", network)
	}
	conn, err := t.dialProxy(ctx)
	if err != nil {
		return nil, fmt.Errorf("http_tunnel: failed dialing to proxy: %v", err)
	}
	req := &http.Request{
		Method: "CONNECT",
		URL:    &url.URL{Opaque: address},
		Host:   address, // This is weird
		Header: make(http.Header),
	}
	if t.auth != nil && t.auth.InitialResponse() != "" {
		req.Header.Set(hdrProxyAuthResp, t.auth.Type()+" "+t.auth.InitialResponse())
	}
	resp, err := t.doRoundtrip(conn, req)
	if err != nil {
		conn.Close()
		return nil, err
	}
	// Retry request with auth, if available.
	if resp.StatusCode == http.StatusProxyAuthRequired && t.auth != nil {
		responseHdr, err := t.performAuthChallengeResponse(resp)
		if err != nil {
			conn.Close()
			return nil, err
		}
		req.Header.Set(hdrProxyAuthResp, t.auth.Type()+" "+responseHdr)
		resp, err = t.doRoundtrip(conn, req)
		if err != nil {
			conn.Close()
			return nil, err
		}
	}

	if resp.StatusCode != 200 {
		conn.Close()
		return nil, fmt.Errorf("http_tunnel: failed proxying %d: %s", resp.StatusCode, resp.Status)
	}

	return conn, nil
}

// Dial is an implementation of net.Dialer, and returns a TCP connection handle to the host that HTTP CONNECT reached.
func (t HttpConnectTunnel) Dial(network string, address string) (net.Conn, error) {
	return t.DialContext(context.Background(), network, address)
}

func (t HttpConnectTunnel) doRoundtrip(conn net.Conn, req *http.Request) (*http.Response, error) {
	if err := req.Write(conn); err != nil {
		return nil, fmt.Errorf("http_tunnel: failed writing request: %v", err)
	}
	// Doesn't matter, discard this bufio.
	br := bufio.NewReader(conn)
	return http.ReadResponse(br, req)
}

func (t HttpConnectTunnel) performAuthChallengeResponse(resp *http.Response) (string, error) {
	respAuthHdr := resp.Header.Get(hdrProxyAuthReq)
	if !strings.Contains(respAuthHdr, t.auth.Type()+" ") {
		return "", fmt.Errorf("http_tunnel: expected '%v' Proxy authentication, got: '%v'", t.auth.Type(), respAuthHdr)
	}
	splits := strings.SplitN(respAuthHdr, " ", 2)
	challenge := splits[1]
	return t.auth.ChallengeResponse(challenge), nil
}

// WARNING: this can leak a goroutine for as long as the underlying Dialer implementation takes to timeout
// A Conn returned from a successful Dial after the context has been cancelled will be immediately closed.
func dialContext(ctx context.Context, d proxy.Dialer, network, address string) (net.Conn, error) {
	var (
		conn net.Conn
		done = make(chan struct{}, 1)
		err  error
	)
	go func() {
		conn, err = d.Dial(network, address)
		close(done)
		if conn != nil && ctx.Err() != nil {
			conn.Close()
		}
	}()
	select {
	case <-ctx.Done():
		err = ctx.Err()
	case <-done:
	}
	return conn, err
}
