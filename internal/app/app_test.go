package app

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/xenomit/server/pkg/protocol/codec"
	"github.com/xenomit/server/pkg/protocol/v10"
)

func TestLoginAndMove(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv, err := Start(ctx, Config{
		GameAddr: "127.0.0.1:0", ChatAddr: "127.0.0.1:0", PolicyAddr: "127.0.0.1:0", HTTPAddr: "127.0.0.1:0",
	})
	if err != nil {
		t.Fatal(err)
	}
	token, userID := login(t, srv.HTTP)
	sid := issue(t, srv.HTTP, token)

	conn, err := net.Dial("tcp", srv.Game)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	frame, err := v10.EncodeLogin(v10.Login{UserID: int32(userID), SessionID: sid, FactionID: 1, Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write(frame); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	if !waitID(t, conn, v10.ShipInitID) {
		t.Fatal("no ship initialization")
	}
	move, err := v10.EncodeMoveRequest(v10.Move{TargetX: 1800, TargetY: 1200, PosX: 1000, PosY: 1000})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write(move); err != nil {
		t.Fatal(err)
	}
	if !waitID(t, conn, v10.MoveCommandID) {
		t.Fatal("no move command")
	}
}

func login(t *testing.T, httpAddr string) (string, int64) {
	t.Helper()
	res, err := http.Post("http://"+httpAddr+"/login", "application/json", strings.NewReader(`{"username":"pilot","password":"pilot"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var body struct {
		Token  string
		UserID int64
		Error  string
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 200 || body.Token == "" {
		t.Fatalf("login %d %s", res.StatusCode, body.Error)
	}
	return body.Token, body.UserID
}

func issue(t *testing.T, httpAddr, token string) string {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, "http://"+httpAddr+"/session", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var body struct {
		SessionID string
		Error     string
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.SessionID == "" {
		t.Fatalf("session: %s", body.Error)
	}
	return body.SessionID
}

func waitID(t *testing.T, conn net.Conn, want int16) bool {
	t.Helper()
	buf := make([]byte, 0, 4096)
	tmp := make([]byte, 4096)
	for {
		n, err := conn.Read(tmp)
		if err != nil {
			if err != io.EOF {
				t.Log(err)
			}
			return false
		}
		buf = append(buf, tmp[:n]...)
		frames, rest, err := codec.SplitFrames(buf)
		if err != nil {
			t.Log(err)
			return false
		}
		buf = rest
		for _, f := range frames {
			id, err := v10.CommandID(f)
			if err == nil && id == want {
				return true
			}
		}
	}
}
