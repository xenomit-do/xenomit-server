package main

import (
	"flag"
	"fmt"
	"net"
	"time"

	"github.com/xenomit/server/pkg/protocol/codec"
	"github.com/xenomit/server/pkg/protocol/v10"
)

// loadbot opens one game connection, logs in, and sends move packets.
func main() {
	addr := flag.String("addr", "127.0.0.1:8080", "game address")
	user := flag.Int("user", 1, "user id")
	session := flag.String("session", "", "session id from POST /session")
	n := flag.Int("n", 20, "move packets")
	flag.Parse()
	if *session == "" {
		fmt.Println("session is required (POST /session)")
		return
	}
	conn, err := net.Dial("tcp", *addr)
	if err != nil {
		panic(err)
	}
	defer conn.Close()
	login, err := v10.EncodeLogin(v10.Login{UserID: int32(*user), SessionID: *session, FactionID: 1, Version: "loadbot"})
	if err != nil {
		panic(err)
	}
	if _, err = conn.Write(login); err != nil {
		panic(err)
	}
	buf := make([]byte, 0, 8192)
	tmp := make([]byte, 4096)
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	got := false
	for !got {
		k, err := conn.Read(tmp)
		if err != nil {
			panic(err)
		}
		buf = append(buf, tmp[:k]...)
		frames, rest, err := codec.SplitFrames(buf)
		if err != nil {
			panic(err)
		}
		buf = rest
		for _, f := range frames {
			id, _ := v10.CommandID(f)
			if id == v10.ShipInitID {
				got = true
			}
		}
	}
	for i := 0; i < *n; i++ {
		x := int32(1000 + i*50)
		frame, err := v10.EncodeMoveRequest(v10.Move{TargetX: x, TargetY: 1000, PosX: x, PosY: 1000})
		if err != nil {
			panic(err)
		}
		if _, err = conn.Write(frame); err != nil {
			panic(err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	fmt.Printf("sent %d moves as user %d\n", *n, *user)
}
