package unixsock

import (
	"context"
	"errors"
	"testing"
	"time"

	runtimeauth "github.com/bamanoz/tabula/internal/runtime/auth"
	"github.com/bamanoz/tabula/internal/runtime/codec"
	runtimeconn "github.com/bamanoz/tabula/internal/runtime/conn"
	"github.com/bamanoz/tabula/internal/runtime/wire"
)

func TestUnixSocketAuthenticatedHandshakeAcceptsAndRejects(t *testing.T) {
	store := runtimeauth.NewMemoryStore()
	if err := store.Set(runtimeauth.TokenRecord{RuntimeID: runtimeauth.LocalRuntimeID, Token: "rtk_good", CreatedAt: time.Now()}); err != nil {
		t.Fatalf("Set token: %v", err)
	}
	sock := shortSocketPath(t)
	listener, err := Listen(sock)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer listener.Close()

	serverDone := make(chan error, 1)
	go func() {
		serverDone <- listener.Serve(func(ctx context.Context, c *codec.Conn) {
			_ = runtimeconn.ServeAuthenticated(ctx, c, authHandler{Authenticator: runtimeauth.Authenticator{Store: store, KernelID: "main"}})
		})
	}()

	bad := dialAndHandshake(t, sock, "rtk_bad")
	if bad.Accepted || bad.Error == nil || bad.Error.Code != wire.ErrorUnauthorized {
		t.Fatalf("bad ack = %#v", bad)
	}
	if bad.Error.Message == "rtk_bad" || bad.Error.Message == "rtk_good" {
		t.Fatalf("auth rejection leaked token: %#v", bad.Error)
	}

	good := dialAndHandshake(t, sock, "rtk_good")
	if !good.Accepted || good.KernelID != "main" || good.Error != nil {
		t.Fatalf("good ack = %#v", good)
	}

	listener.Close()
	select {
	case <-serverDone:
	case <-time.After(time.Second):
		t.Fatal("listener did not stop")
	}
}

func TestAuthenticatedHandshakeRejectsOldTokenAfterRegeneration(t *testing.T) {
	store := runtimeauth.NewMemoryStore()
	path := runtimeauth.RuntimeTokenPath(t.TempDir())
	first, err := runtimeauth.IssueLocalTokenFile(store, path, runtimeauth.LocalRuntimeID, time.Unix(1, 0))
	if err != nil {
		t.Fatalf("first token: %v", err)
	}
	second, err := runtimeauth.IssueLocalTokenFile(store, path, runtimeauth.LocalRuntimeID, time.Unix(2, 0))
	if err != nil {
		t.Fatalf("second token: %v", err)
	}
	if err := store.Validate(runtimeauth.LocalRuntimeID, first.Token); !errors.Is(err, runtimeauth.ErrUnauthorized) {
		t.Fatalf("old token Validate = %v, want unauthorized", err)
	}
	if err := store.Validate(runtimeauth.LocalRuntimeID, second.Token); err != nil {
		t.Fatalf("second token Validate: %v", err)
	}
}

type authHandler struct {
	runtimeauth.Authenticator
	socketHandler
}

func (h authHandler) Hello(_ context.Context, hello wire.Hello) (wire.HelloAck, error) {
	return h.Authenticator.HelloAck(hello), nil
}

func dialAndHandshake(t *testing.T, sock, token string) wire.HelloAck {
	t.Helper()
	c, err := Dial(context.Background(), "unix://"+sock)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() { _ = c.CloseNow() }()
	ack, err := runtimeconn.Handshake(context.Background(), c, wire.Hello{Op: wire.OpHello, RuntimeID: runtimeauth.LocalRuntimeID, Token: token, ProtocolVersion: "1"})
	if err != nil {
		t.Fatalf("Handshake: %v", err)
	}
	return ack
}
