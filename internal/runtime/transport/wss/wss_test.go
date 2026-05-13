package wss

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	runtimeapi "github.com/bamanoz/tabula/internal/runtime"
	"github.com/bamanoz/tabula/internal/runtime/codec"
	runtimeconn "github.com/bamanoz/tabula/internal/runtime/conn"
	"github.com/bamanoz/tabula/internal/runtime/wire"
)

func TestWSSTransportRuntimeConnRoundTrip(t *testing.T) {
	mux := http.NewServeMux()
	accepted := make(chan struct{})
	Listener{}.Mount(mux, func(ctx context.Context, c *codec.Conn) {
		close(accepted)
		_ = runtimeconn.ServeAuthenticated(ctx, c, wssHandler{})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	conn, err := Dial(context.Background(), "ws"+server.URL[len("http"):]+DefaultPath, DialOptions{})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() { _ = conn.CloseNow() }()
	waitFor(t, accepted, "runtime websocket accept")

	ack, err := runtimeconn.Handshake(context.Background(), conn, wire.Hello{Op: wire.OpHello, RuntimeID: "remote", Token: "redacted", ProtocolVersion: "1"})
	if err != nil {
		t.Fatalf("Handshake: %v", err)
	}
	if !ack.Accepted || ack.KernelID != "main" {
		t.Fatalf("unexpected ack: %#v", ack)
	}
	rc := runtimeconn.New(conn)

	resp, err := rc.Invoke(context.Background(), runtimeapi.InvokeReq{CallID: "call-1", TenantID: "default", Target: pluginTarget("fs"), Tool: "echo", Args: json.RawMessage(`{"ok":true}`)})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if !resp.OK || string(resp.Data) != `{"echo":{"ok":true}}` {
		t.Fatalf("unexpected invoke response: %#v", resp)
	}
	health, err := rc.Health(context.Background())
	if err != nil || !health.OK || health.WorkerCount != 1 {
		t.Fatalf("Health = %#v, %v", health, err)
	}
	caps, err := rc.ListCapabilities(context.Background())
	if err != nil || len(caps.Targets) != 1 || caps.Targets[0].Target.ID != "fs" {
		t.Fatalf("ListCapabilities = %#v, %v", caps, err)
	}
}

func TestWSSTransportDisconnectDrainsPendingInvokeAsRuntimeUnavailable(t *testing.T) {
	mux := http.NewServeMux()
	accepted := make(chan struct{})
	serverConn := make(chan *codec.Conn, 1)
	muxClosed := make(chan struct{})
	Listener{}.Mount(mux, func(ctx context.Context, c *codec.Conn) {
		close(accepted)
		serverConn <- c
		_ = runtimeconn.ServeAuthenticated(ctx, c, wssLongHandler{entered: muxClosed})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	conn, err := Dial(context.Background(), "ws"+server.URL[len("http"):]+DefaultPath, DialOptions{})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() { _ = conn.CloseNow() }()
	waitFor(t, accepted, "runtime websocket accept")
	if _, err := runtimeconn.Handshake(context.Background(), conn, wire.Hello{Op: wire.OpHello, RuntimeID: "remote", Token: "redacted", ProtocolVersion: "1"}); err != nil {
		t.Fatalf("Handshake: %v", err)
	}
	rc := runtimeconn.New(conn)
	done := make(chan runtimeapi.InvokeResp, 1)
	go func() {
		resp, err := rc.Invoke(context.Background(), runtimeapi.InvokeReq{CallID: "call-long", TenantID: "default", Target: pluginTarget("fs"), Tool: "long"})
		if err != nil {
			t.Errorf("Invoke long: %v", err)
			return
		}
		done <- resp
	}()
	waitFor(t, muxClosed, "long invoke start")
	_ = (<-serverConn).CloseNow()
	select {
	case resp := <-done:
		if resp.OK || resp.Error == nil || resp.Error.Code != wire.ErrorRuntimeUnavailable || !resp.Error.Retryable {
			t.Fatalf("expected runtime_unavailable retryable, got %#v", resp)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for drained invoke after disconnect")
	}
}

func TestWSSOriginAllowlistRejectsUnexpectedOrigin(t *testing.T) {
	mux := http.NewServeMux()
	Listener{Origins: []string{"https://allowed.example"}}.Mount(mux, func(context.Context, *codec.Conn) {})
	server := httptest.NewServer(mux)
	defer server.Close()

	_, _, err := websocket.Dial(context.Background(), "ws"+server.URL[len("http"):]+DefaultPath, &websocket.DialOptions{
		Subprotocols: []string{codec.Subprotocol},
		HTTPHeader:   http.Header{"Origin": {"https://evil.example"}},
	})
	if err == nil {
		t.Fatal("expected origin rejection")
	}
	if got := websocket.CloseStatus(err); got != -1 {
		t.Fatalf("expected HTTP rejection before websocket close, got status %d err=%v", got, err)
	}
}

func TestWSSSubprotocolMismatchClosesProtocolError(t *testing.T) {
	mux := http.NewServeMux()
	called := make(chan struct{}, 1)
	Listener{}.Mount(mux, func(context.Context, *codec.Conn) { called <- struct{}{} })
	server := httptest.NewServer(mux)
	defer server.Close()

	ws, _, err := websocket.Dial(context.Background(), "ws"+server.URL[len("http"):]+DefaultPath, &websocket.DialOptions{Subprotocols: []string{"wrong.v1"}})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() { _ = ws.CloseNow() }()
	if _, _, err := codec.New(ws).Read(context.Background()); websocket.CloseStatus(err) != websocket.StatusProtocolError {
		t.Fatalf("expected protocol-error close, got status=%d err=%v", websocket.CloseStatus(err), err)
	}
	select {
	case <-called:
		t.Fatal("handler should not run on subprotocol mismatch")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestWSSMTLSHandshakeSucceedsWithValidCertAndToken(t *testing.T) {
	files := writeTLSFixture(t)
	mux := http.NewServeMux()
	accepted := make(chan struct{})
	Listener{}.Mount(mux, func(ctx context.Context, c *codec.Conn) {
		close(accepted)
		_ = runtimeconn.ServeAuthenticated(ctx, c, wssHandler{})
	})
	server := httptest.NewUnstartedServer(mux)
	server.TLS = mustServerTLSConfig(t, files.serverCert, files.serverKey, files.clientCA, ClientCertAuthRequire)
	server.StartTLS()
	defer server.Close()

	conn, err := Dial(context.Background(), "wss"+server.URL[len("https"):]+DefaultPath, DialOptions{CAFile: files.serverCA, CertFile: files.clientCert, KeyFile: files.clientKey})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() { _ = conn.CloseNow() }()
	waitFor(t, accepted, "mTLS accept")
	ack, err := runtimeconn.Handshake(context.Background(), conn, wire.Hello{Op: wire.OpHello, RuntimeID: "remote", Token: "redacted", ProtocolVersion: "1"})
	if err != nil {
		t.Fatalf("Handshake: %v", err)
	}
	if !ack.Accepted {
		t.Fatalf("expected accepted hello ack, got %#v", ack)
	}
}

func TestWSSMTLSRejectsInvalidClientChainBeforeUpgrade(t *testing.T) {
	files := writeTLSFixture(t)
	other := writeTLSFixture(t)
	mux := http.NewServeMux()
	Listener{}.Mount(mux, func(context.Context, *codec.Conn) { t.Fatal("handler should not run") })
	server := httptest.NewUnstartedServer(mux)
	server.TLS = mustServerTLSConfig(t, files.serverCert, files.serverKey, files.clientCA, ClientCertAuthRequire)
	server.StartTLS()
	defer server.Close()

	certificate, err := tls.LoadX509KeyPair(other.clientCert, other.clientKey)
	if err != nil {
		t.Fatalf("LoadX509KeyPair: %v", err)
	}
	pool := x509.NewCertPool()
	pemData, err := os.ReadFile(files.serverCA)
	if err != nil {
		t.Fatalf("ReadFile serverCA: %v", err)
	}
	pool.AppendCertsFromPEM(pemData)
	_, _, err = websocket.Dial(context.Background(), "wss"+server.URL[len("https"):]+DefaultPath, &websocket.DialOptions{
		Subprotocols: []string{codec.Subprotocol},
		HTTPClient: &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
			RootCAs:      pool,
			Certificates: []tls.Certificate{certificate},
		}}},
	})
	if err == nil {
		t.Fatal("expected TLS failure for invalid client chain")
	}
}

func TestWSSClientAuthRequestAllowsCertlessClients(t *testing.T) {
	files := writeTLSFixture(t)
	mux := http.NewServeMux()
	accepted := make(chan struct{})
	Listener{}.Mount(mux, func(ctx context.Context, c *codec.Conn) {
		close(accepted)
		_ = runtimeconn.ServeAuthenticated(ctx, c, wssHandler{})
	})
	server := httptest.NewUnstartedServer(mux)
	server.TLS = mustServerTLSConfig(t, files.serverCert, files.serverKey, files.clientCA, ClientCertAuthRequest)
	server.StartTLS()
	defer server.Close()

	conn, err := Dial(context.Background(), "wss"+server.URL[len("https"):]+DefaultPath, DialOptions{CAFile: files.serverCA})
	if err != nil {
		t.Fatalf("Dial certless request mode: %v", err)
	}
	defer func() { _ = conn.CloseNow() }()
	waitFor(t, accepted, "certless request-mode accept")
}

func TestWSSClientAuthRequireRejectsCertlessClients(t *testing.T) {
	files := writeTLSFixture(t)
	mux := http.NewServeMux()
	Listener{}.Mount(mux, func(context.Context, *codec.Conn) { t.Fatal("handler should not run") })
	server := httptest.NewUnstartedServer(mux)
	server.TLS = mustServerTLSConfig(t, files.serverCert, files.serverKey, files.clientCA, ClientCertAuthRequire)
	server.StartTLS()
	defer server.Close()

	_, err := Dial(context.Background(), "wss"+server.URL[len("https"):]+DefaultPath, DialOptions{CAFile: files.serverCA})
	if err == nil {
		t.Fatal("expected certless client to be rejected in require mode")
	}
}

func TestWSSUnknownRuntimeValidatorRejectsStructuredError(t *testing.T) {
	files := writeTLSFixture(t)
	mux := http.NewServeMux()
	Listener{ClientCertValidator: func(cert *x509.Certificate) error {
		if cert.Subject.CommonName == "unknown-runtime" {
			return &RejectError{Status: http.StatusUnauthorized, Code: string(wire.ErrorUnknownRuntime), Message: "runtime \"unknown-runtime\" is not configured"}
		}
		return nil
	}}.Mount(mux, func(context.Context, *codec.Conn) { t.Fatal("handler should not run") })
	server := httptest.NewUnstartedServer(mux)
	server.TLS = mustServerTLSConfig(t, files.serverCert, files.serverKey, files.clientCA, ClientCertAuthRequest)
	server.StartTLS()
	defer server.Close()

	certificate, err := tls.LoadX509KeyPair(files.unknownClientCert, files.unknownClientKey)
	if err != nil {
		t.Fatalf("LoadX509KeyPair unknown: %v", err)
	}
	pool := x509.NewCertPool()
	pemData, err := os.ReadFile(files.serverCA)
	if err != nil {
		t.Fatalf("ReadFile serverCA: %v", err)
	}
	pool.AppendCertsFromPEM(pemData)
	_, resp, err := websocket.Dial(context.Background(), "wss"+server.URL[len("https"):]+DefaultPath, &websocket.DialOptions{
		Subprotocols: []string{codec.Subprotocol},
		HTTPClient: &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
			RootCAs:      pool,
			Certificates: []tls.Certificate{certificate},
		}}},
	})
	if err == nil {
		t.Fatal("expected structured unknown_runtime rejection")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 response, got resp=%v err=%v", resp, err)
	}
	body, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		t.Fatalf("ReadAll response body: %v", readErr)
	}
	if string(body) == "" || !containsAll(string(body), `"code":"unknown_runtime"`, `unknown-runtime`) {
		t.Fatalf("unexpected rejection body: %s", string(body))
	}
}

func TestWSSDefaultKeepAliveSendsObservablePing(t *testing.T) {
	oldInterval := defaultKeepAliveInterval
	oldHook := keepAlivePingHook
	t.Cleanup(func() {
		defaultKeepAliveInterval = oldInterval
		keepAlivePingHook = oldHook
	})
	defaultKeepAliveInterval = 10 * time.Millisecond
	pinged := make(chan struct{}, 1)
	keepAlivePingHook = func() {
		select {
		case pinged <- struct{}{}:
		default:
		}
	}

	mux := http.NewServeMux()
	accepted := make(chan struct{})
	Listener{}.Mount(mux, func(_ context.Context, c *codec.Conn) {
		close(accepted)
		go func() {
			for {
				_, reader, err := c.WebSocket().Reader(context.Background())
				if err != nil {
					return
				}
				_, _ = io.Copy(io.Discard, reader)
			}
		}()
		<-time.After(250 * time.Millisecond)
		_ = c.CloseNow()
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	conn, err := Dial(context.Background(), "ws"+server.URL[len("http"):]+DefaultPath, DialOptions{})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() { _ = conn.CloseNow() }()
	waitFor(t, accepted, "runtime websocket accept")
	select {
	case <-pinged:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for default keepalive ping")
	}
}

type wssHandler struct{}

func (wssHandler) Hello(context.Context, wire.Hello) (wire.HelloAck, error) {
	return wire.HelloAck{Op: wire.OpHelloAck, Accepted: true, KernelID: "main"}, nil
}

func (wssHandler) Invoke(_ context.Context, in wire.Invoke) (wire.InvokeResult, error) {
	return wire.InvokeResult{Op: wire.OpInvokeResult, CallID: in.CallID, OK: true, Data: json.RawMessage(fmt.Sprintf(`{"echo":%s}`, string(in.Args)))}, nil
}

func (wssHandler) Cancel(context.Context, wire.Cancel) (wire.CancelAck, error) {
	return wire.CancelAck{Op: wire.OpCancelAck, CallID: "unused"}, nil
}

func (wssHandler) Health(context.Context, wire.Health) (wire.HealthResp, error) {
	return wire.HealthResp{Op: wire.OpHealthResp, OK: true, WorkerCount: 1}, nil
}

func (wssHandler) ListCapabilities(context.Context, wire.ListCapabilities) (wire.ListCapabilitiesResp, error) {
	return wire.ListCapabilitiesResp{Op: wire.OpListCapabilitiesResp, Targets: []wire.Capability{{Target: pluginTarget("fs"), Tools: []wire.ToolSpec{{Name: "echo"}}, State: wire.CapabilityStateReady, Source: wire.CapabilitySourceWorker}}}, nil
}

func (wssHandler) Reload(context.Context, wire.Reload) (wire.ReloadAck, error) {
	return wire.ReloadAck{Op: wire.OpReloadAck}, nil
}

func (wssHandler) HookEvent(context.Context, wire.HookEvent) (wire.HookEventReply, error) {
	return wire.HookEventReply{Op: wire.OpHookEventReply, Action: wire.HookActionOK}, nil
}

type wssLongHandler struct{ entered chan struct{} }

func (h wssLongHandler) Hello(ctx context.Context, hello wire.Hello) (wire.HelloAck, error) {
	return wssHandler{}.Hello(ctx, hello)
}

func (h wssLongHandler) Invoke(_ context.Context, in wire.Invoke) (wire.InvokeResult, error) {
	if in.Tool == "long" {
		close(h.entered)
		select {}
	}
	return wssHandler{}.Invoke(context.Background(), in)
}

func (h wssLongHandler) Cancel(ctx context.Context, in wire.Cancel) (wire.CancelAck, error) {
	return wssHandler{}.Cancel(ctx, in)
}

func (h wssLongHandler) Health(ctx context.Context, in wire.Health) (wire.HealthResp, error) {
	return wssHandler{}.Health(ctx, in)
}

func (h wssLongHandler) ListCapabilities(ctx context.Context, in wire.ListCapabilities) (wire.ListCapabilitiesResp, error) {
	return wssHandler{}.ListCapabilities(ctx, in)
}

func (h wssLongHandler) Reload(ctx context.Context, in wire.Reload) (wire.ReloadAck, error) {
	return wssHandler{}.Reload(ctx, in)
}

func (h wssLongHandler) HookEvent(ctx context.Context, in wire.HookEvent) (wire.HookEventReply, error) {
	return wssHandler{}.HookEvent(ctx, in)
}

func pluginTarget(id string) wire.Target { return wire.Target{Kind: wire.TargetKindPlugin, ID: id} }

func waitFor(t *testing.T, ch <-chan struct{}, label string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s", label)
	}
}

type tlsFixtureFiles struct {
	serverCA          string
	clientCA          string
	serverCert        string
	serverKey         string
	clientCert        string
	clientKey         string
	unknownClientCert string
	unknownClientKey  string
}

func mustServerTLSConfig(t *testing.T, certFile, keyFile, clientCA string, mode ClientCertAuthMode) *tls.Config {
	t.Helper()
	config, err := LoadServerTLSConfig(certFile, keyFile, clientCA, mode)
	if err != nil {
		t.Fatalf("LoadServerTLSConfig: %v", err)
	}
	return config
}

func writeTLSFixture(t *testing.T) tlsFixtureFiles {
	t.Helper()
	dir := t.TempDir()
	serverCA := mustCreateCA(t, "tabula-server-ca")
	clientCA := mustCreateCA(t, "tabula-client-ca")
	serverLeaf := mustCreateSignedCert(t, serverCA, "127.0.0.1", true)
	clientLeaf := mustCreateSignedCert(t, clientCA, "remote", false)
	unknownLeaf := mustCreateSignedCert(t, clientCA, "unknown-runtime", false)
	return tlsFixtureFiles{
		serverCA:          writeCertPEM(t, filepath.Join(dir, "server-ca.pem"), serverCA.certPEM),
		clientCA:          writeCertPEM(t, filepath.Join(dir, "client-ca.pem"), clientCA.certPEM),
		serverCert:        writeCertPEM(t, filepath.Join(dir, "server-cert.pem"), serverLeaf.certPEM),
		serverKey:         writeCertPEM(t, filepath.Join(dir, "server-key.pem"), serverLeaf.keyPEM),
		clientCert:        writeCertPEM(t, filepath.Join(dir, "client-cert.pem"), clientLeaf.certPEM),
		clientKey:         writeCertPEM(t, filepath.Join(dir, "client-key.pem"), clientLeaf.keyPEM),
		unknownClientCert: writeCertPEM(t, filepath.Join(dir, "unknown-client-cert.pem"), unknownLeaf.certPEM),
		unknownClientKey:  writeCertPEM(t, filepath.Join(dir, "unknown-client-key.pem"), unknownLeaf.keyPEM),
	}
}

type generatedCA struct {
	cert    *x509.Certificate
	key     *rsa.PrivateKey
	certPEM []byte
}

type generatedCert struct {
	certPEM []byte
	keyPEM  []byte
}

func mustCreateCA(t *testing.T, cn string) generatedCA {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey CA: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: cn},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("CreateCertificate CA: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("ParseCertificate CA: %v", err)
	}
	return generatedCA{cert: cert, key: key, certPEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})}
}

func mustCreateSignedCert(t *testing.T, ca generatedCA, cn string, server bool) generatedCert {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey cert: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
	}
	if server {
		tmpl.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
		tmpl.IPAddresses = []net.IP{net.ParseIP("127.0.0.1")}
	} else {
		tmpl.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.cert, &key.PublicKey, ca.key)
	if err != nil {
		t.Fatalf("CreateCertificate signed: %v", err)
	}
	return generatedCert{
		certPEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		keyPEM:  pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}),
	}
}

func writeCertPEM(t *testing.T, path string, data []byte) string {
	t.Helper()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile %s: %v", path, err)
	}
	return path
}

func containsAll(s string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(s, part) {
			return false
		}
	}
	return true
}
