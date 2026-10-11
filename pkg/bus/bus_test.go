package bus

import "testing"

func TestWildcard(t *testing.T) {
	b := New()
	var got []string
	b.Subscribe("out.>", func(subject string, _ []byte) {
		got = append(got, subject)
	})
	b.Subscribe("in.*", func(subject string, data []byte) {
		got = append(got, subject+":"+string(data))
	})
	b.Publish("in.1", []byte("move"))
	b.Publish("out.abc", []byte("x"))
	b.Publish("chat.msg", []byte("no"))
	if len(got) != 2 || got[0] != "in.1:move" || got[1] != "out.abc" {
		t.Fatalf("%v", got)
	}
}
