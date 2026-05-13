// Package wss provides Runtime API websocket transport over ws:// and wss://.
package wss

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/coder/websocket"

	"github.com/bamanoz/tabula/internal/runtime/codec"
)

const (
	// DefaultPath is the kernel runtime websocket path.
	DefaultPath = "/runtime"
	// DefaultKeepAliveInterval sends websocket pings often enough to keep the
	// Runtime API transport warm under the default idle read timeout.
	DefaultKeepAliveInterval = 30 * time.Second
)

var (
	defaultKeepAliveInterval = DefaultKeepAliveInterval
	keepAlivePingHook        func()
)

// Handler accepts one websocket Runtime API connection.
type Handler func(context.Context, *codec.Conn)

// Listener mounts a Runtime API websocket endpoint on an existing HTTP mux.
type Listener struct {
	Path                string
	Origins             []string
	KeepAliveInterval   time.Duration
	Logger              *slog.Logger
	ClientCertValidator func(*x509.Certificate) error
}

// DialOptions configures one ws/wss dial.
type DialOptions struct {
	TLSInsecureSkipVerify bool
	CAFile                string
	CertFile              string
	KeyFile               string
	HTTPHeader            http.Header
}

type contextKey string

const peerCertCNContextKey contextKey = "runtime-wss-peer-cert-cn"

type ClientCertAuthMode string

const (
	ClientCertAuthNone    ClientCertAuthMode = "none"
	ClientCertAuthRequest ClientCertAuthMode = "request"
	ClientCertAuthRequire ClientCertAuthMode = "require"
)

type RejectError struct {
	Status  int
	Code    string
	Message string
}

func (e *RejectError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

// Mount registers the runtime websocket endpoint on mux.
func (l Listener) Mount(mux *http.ServeMux, handler Handler) {
	if mux == nil {
		panic("runtime wss mux is nil")
	}
	if handler == nil {
		panic("runtime wss handler is nil")
	}
	path := strings.TrimSpace(l.Path)
	if path == "" {
		path = DefaultPath
	}
	mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		if !originAllowed(strings.TrimSpace(r.Header.Get("Origin")), l.Origins) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		ctx := context.Background()
		if cert := peerLeaf(r.TLS); cert != nil {
			if l.ClientCertValidator != nil {
				if err := l.ClientCertValidator(cert); err != nil {
					writeReject(w, err)
					return
				}
			}
			ctx = context.WithValue(ctx, peerCertCNContextKey, strings.TrimSpace(cert.Subject.CommonName))
		}
		ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			Subprotocols:       []string{codec.Subprotocol},
			InsecureSkipVerify: true,
		})
		if err != nil {
			l.logger().Warn("runtime websocket accept failed", "error", err)
			return
		}
		if ws.Subprotocol() != codec.Subprotocol {
			l.logger().Warn("runtime websocket subprotocol mismatch", "got", ws.Subprotocol(), "want", codec.Subprotocol)
			_ = ws.Close(websocket.StatusProtocolError, "runtime websocket subprotocol required")
			return
		}
		startKeepAlive(ws, l.keepAliveInterval(), l.logger())
		go handler(ctx, codec.New(ws))
	})
}

// Dial opens a Runtime API websocket connection to a kernel /runtime endpoint.
func Dial(ctx context.Context, rawURL string, opts DialOptions) (*codec.Conn, error) {
	transport := &http.Transport{}
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(rawURL)), "wss://") {
		tlsConfig, err := LoadClientTLSConfig(opts)
		if err != nil {
			return nil, err
		}
		transport.TLSClientConfig = tlsConfig
	}
	dialOpts := &websocket.DialOptions{
		HTTPClient: &http.Client{Transport: transport},
		HTTPHeader: opts.HTTPHeader,
	}
	conn, _, err := codec.Dial(ctx, rawURL, dialOpts)
	if err != nil {
		return nil, err
	}
	startKeepAlive(conn.WebSocket(), defaultKeepAliveInterval, slog.Default())
	return conn, nil
}

func LoadClientTLSConfig(opts DialOptions) (*tls.Config, error) {
	config := &tls.Config{InsecureSkipVerify: opts.TLSInsecureSkipVerify} //nolint:gosec // explicit operator/test setting
	if strings.TrimSpace(opts.CAFile) != "" {
		pem, err := os.ReadFile(opts.CAFile)
		if err != nil {
			return nil, fmt.Errorf("read runtime ca_file %s: %w", opts.CAFile, err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("parse runtime ca_file %s: no certificates found", opts.CAFile)
		}
		config.RootCAs = pool
	}
	certFile := strings.TrimSpace(opts.CertFile)
	keyFile := strings.TrimSpace(opts.KeyFile)
	if (certFile == "") != (keyFile == "") {
		return nil, fmt.Errorf("runtime cert_file and key_file must be set together")
	}
	if certFile != "" {
		certificate, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			return nil, fmt.Errorf("load runtime client certificate: %w", err)
		}
		config.Certificates = []tls.Certificate{certificate}
	}
	return config, nil
}

func LoadServerTLSConfig(certFile, keyFile, clientCAFile string, clientAuth ClientCertAuthMode) (*tls.Config, error) {
	certFile = strings.TrimSpace(certFile)
	keyFile = strings.TrimSpace(keyFile)
	clientCAFile = strings.TrimSpace(clientCAFile)
	if (certFile == "") != (keyFile == "") {
		return nil, fmt.Errorf("runtime wss cert_file and key_file must be set together")
	}
	if certFile == "" {
		return nil, nil
	}
	certificate, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("load runtime wss certificate: %w", err)
	}
	mode, err := normalizeClientAuth(clientAuth, clientCAFile != "")
	if err != nil {
		return nil, err
	}
	config := &tls.Config{
		Certificates: []tls.Certificate{certificate},
		MinVersion:   tls.VersionTLS12,
	}
	if clientCAFile != "" {
		pem, err := os.ReadFile(clientCAFile)
		if err != nil {
			return nil, fmt.Errorf("read runtime client_ca %s: %w", clientCAFile, err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("parse runtime client_ca %s: no certificates found", clientCAFile)
		}
		config.ClientCAs = pool
	}
	config.ClientAuth = mode
	return config, nil
}

func PeerCertCN(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	value, _ := ctx.Value(peerCertCNContextKey).(string)
	return strings.TrimSpace(value)
}

func originAllowed(origin string, allowed []string) bool {
	if origin == "" {
		return true
	}
	if len(allowed) == 0 {
		return true
	}
	for _, candidate := range allowed {
		if strings.EqualFold(strings.TrimSpace(candidate), origin) {
			return true
		}
	}
	return false
}

func startKeepAlive(ws *websocket.Conn, interval time.Duration, logger *slog.Logger) {
	if ws == nil {
		return
	}
	if interval <= 0 {
		interval = DefaultKeepAliveInterval
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			if keepAlivePingHook != nil {
				keepAlivePingHook()
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			err := ws.Ping(ctx)
			cancel()
			if err != nil {
				if logger != nil {
					logger.Debug("runtime websocket keepalive stopped", "error", err)
				}
				return
			}
		}
	}()
}

func (l Listener) keepAliveInterval() time.Duration {
	if l.KeepAliveInterval > 0 {
		return l.KeepAliveInterval
	}
	return DefaultKeepAliveInterval
}

func (l Listener) logger() *slog.Logger {
	if l.Logger != nil {
		return l.Logger
	}
	return slog.Default()
}

// ValidateURL reports whether rawURL uses a supported runtime websocket scheme.
func ValidateURL(rawURL string) error {
	rawURL = strings.ToLower(strings.TrimSpace(rawURL))
	if strings.HasPrefix(rawURL, "ws://") || strings.HasPrefix(rawURL, "wss://") {
		return nil
	}
	return fmt.Errorf("unsupported runtime websocket url %q", rawURL)
}

func normalizeClientAuth(mode ClientCertAuthMode, hasClientCA bool) (tls.ClientAuthType, error) {
	switch strings.ToLower(strings.TrimSpace(string(mode))) {
	case "", string(ClientCertAuthRequest):
		if hasClientCA {
			return tls.VerifyClientCertIfGiven, nil
		}
		return tls.NoClientCert, nil
	case string(ClientCertAuthRequire):
		if !hasClientCA {
			return tls.NoClientCert, fmt.Errorf("runtime wss client_auth=require requires client_ca")
		}
		return tls.RequireAndVerifyClientCert, nil
	case string(ClientCertAuthNone):
		return tls.NoClientCert, nil
	default:
		return tls.NoClientCert, fmt.Errorf("unsupported runtime wss client_auth %q", mode)
	}
}

func peerLeaf(state *tls.ConnectionState) *x509.Certificate {
	if state == nil || len(state.PeerCertificates) == 0 {
		return nil
	}
	return state.PeerCertificates[0]
}

func writeReject(w http.ResponseWriter, err error) {
	reject := &RejectError{Status: http.StatusUnauthorized, Code: "unauthorized", Message: err.Error()}
	if typed, ok := err.(*RejectError); ok && typed != nil {
		reject = typed
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(reject.Status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": reject.Code, "message": reject.Message, "retryable": false}})
}
