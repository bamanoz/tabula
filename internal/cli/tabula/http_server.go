package tabula

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/bamanoz/tabula/internal/kernel"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: checkWebSocketOrigin,
}

const (
	serverReadHeaderTimeout = 5 * time.Second
	serverReadTimeout       = 30 * time.Second
	serverWriteTimeout      = 60 * time.Second
	serverIdleTimeout       = 120 * time.Second
)

func newKernelHTTPServer(handler http.Handler, tlsConfig *tls.Config) *http.Server {
	return &http.Server{
		Handler:           handler,
		TLSConfig:         tlsConfig,
		ReadHeaderTimeout: serverReadHeaderTimeout,
		ReadTimeout:       serverReadTimeout,
		WriteTimeout:      serverWriteTimeout,
		IdleTimeout:       serverIdleTimeout,
	}
}

func registerKernelHTTPHandlers(mux *http.ServeMux, hub *kernel.Hub, listenerHost string, build BuildInfo, managedLocal bool) {
	build = normalizeBuildInfo(build)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":                      "ok",
			"version":                     build.Version,
			"kernel_version":              build.Version,
			"commit":                      build.Commit,
			"protocol_version":            kernel.ClientProtocolVersion,
			"min_plugin_protocol_version": kernel.MinPluginProtocolVersion,
			"max_plugin_protocol_version": kernel.MaxPluginProtocolVersion,
		})
	})

	mux.HandleFunc("/sessions", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(hub.SnapshotSessions())
	})

	mux.HandleFunc("/internal/snapshot/runtimes", internalDiagnosticsGuard(listenerHost, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(hub.SnapshotRuntimes())
	}))

	mux.HandleFunc("/internal/reload/runtime", internalDiagnosticsGuard(listenerHost, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		if err := reloadRuntimeFromConfig(ctx, os.Getenv("TABULA_HOME"), hub, managedLocal); err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}))
}

func internalDiagnosticsGuard(listenerHost string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !isLocalInternalDiagnosticsRequest(r, listenerHost) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next(w, r)
	}
}

func isLocalInternalDiagnosticsRequest(r *http.Request, listenerHost string) bool {
	if r == nil {
		return false
	}
	remoteHost, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err != nil || !isLoopbackHost(remoteHost) {
		return false
	}

	requestHost := normalizeHostOnly(r.Host)
	if isLoopbackHost(requestHost) || strings.EqualFold(requestHost, "localhost") {
		return true
	}

	// If the listener itself was explicitly bound to a loopback host, allow the
	// normalized listener host as an equivalent Host header. Wildcard binds do not
	// authorize public-looking Host values.
	listenerHost = normalizeHostOnly(listenerHost)
	return listenerHost != "" && !isWildcardHost(listenerHost) && isLoopbackHost(listenerHost) && strings.EqualFold(requestHost, listenerHost)
}

func normalizeHostOnly(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if host, _, err := net.SplitHostPort(raw); err == nil {
		return strings.ToLower(strings.Trim(host, "[]"))
	}
	return strings.ToLower(strings.Trim(raw, "[]"))
}

func isLoopbackHost(host string) bool {
	host = strings.ToLower(strings.Trim(strings.TrimSpace(host), "[]"))
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func isWildcardHost(host string) bool {
	host = strings.ToLower(strings.Trim(strings.TrimSpace(host), "[]"))
	return host == "" || host == "0.0.0.0" || host == "::"
}

func checkWebSocketOrigin(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}

	allowed := allowedWebSocketOrigins()
	if len(allowed) == 0 {
		return isLocalOrigin(origin, r.Host)
	}
	for _, candidate := range allowed {
		if strings.EqualFold(origin, candidate) {
			return true
		}
	}
	return false
}

func allowedWebSocketOrigins() []string {
	raw := strings.TrimSpace(os.Getenv("TABULA_ALLOWED_ORIGINS"))
	if raw == "" {
		return nil
	}

	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func isLocalOrigin(origin, requestHost string) bool {
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}

	host := strings.ToLower(u.Hostname())
	switch host {
	case "localhost", "127.0.0.1", "::1":
		return true
	}

	if requestHost == "" {
		return false
	}

	reqURL, err := url.Parse("http://" + requestHost)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Hostname(), reqURL.Hostname())
}
