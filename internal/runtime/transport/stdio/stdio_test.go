package stdio

import (
	"context"
	"io"
	"testing"

	runtimeconn "github.com/bamanoz/tabula/internal/runtime/conn"
	"github.com/bamanoz/tabula/internal/runtime/wire"
)

func TestStdioConnRoundTrip(t *testing.T) {
	aRead, bWrite := io.Pipe()
	bRead, aWrite := io.Pipe()
	client := NewConn(bRead, bWrite)
	server := NewConn(aRead, aWrite)
	defer func() { _ = client.CloseNow() }()
	defer func() { _ = server.CloseNow() }()
	go func() {
		_, frame, err := server.Read(context.Background())
		if err != nil {
			t.Errorf("server read: %v", err)
			return
		}
		hello, ok := frame.(*wire.Hello)
		if !ok || hello.RuntimeID != "remote" {
			t.Errorf("unexpected frame: %#v", frame)
			return
		}
		if err := server.Write(context.Background(), wire.HelloAck{Op: wire.OpHelloAck, Accepted: true, KernelID: "main"}); err != nil {
			t.Errorf("server write: %v", err)
		}
	}()
	ack, err := runtimeconn.Handshake(context.Background(), client, wire.Hello{Op: wire.OpHello, RuntimeID: "remote", Token: "rtk", ProtocolVersion: "1"})
	if err != nil {
		t.Fatalf("Handshake: %v", err)
	}
	if !ack.Accepted || ack.KernelID != "main" {
		t.Fatalf("ack = %#v", ack)
	}
}
