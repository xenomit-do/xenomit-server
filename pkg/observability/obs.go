// Package observability records counters and tick timings and renders them
// as Prometheus text. Spans are a seam for an OpenTelemetry exporter.
package observability

import (
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"
)

// Obs holds process-wide counters.
type Obs struct {
	PacketsIn  atomic.Int64
	PacketsOut atomic.Int64
	Sessions   atomic.Int64
	Logins     atomic.Int64
	Kills      atomic.Int64
	TickP99US  atomic.Int64
	tickMu     sync.Mutex
	ticks      []int64
}

func New() *Obs { return &Obs{} }

// ObserveTick records one shard step duration.
func (o *Obs) ObserveTick(d time.Duration) {
	us := d.Microseconds()
	o.tickMu.Lock()
	o.ticks = append(o.ticks, us)
	if len(o.ticks) > 256 {
		o.ticks = o.ticks[len(o.ticks)-256:]
	}
	cp := append([]int64(nil), o.ticks...)
	o.tickMu.Unlock()
	// Insertion sort; the window is tiny.
	for i := 1; i < len(cp); i++ {
		j := i
		for j > 0 && cp[j] < cp[j-1] {
			cp[j], cp[j-1] = cp[j-1], cp[j]
			j--
		}
	}
	if len(cp) == 0 {
		return
	}
	idx := (len(cp)*99 + 99) / 100
	if idx >= len(cp) {
		idx = len(cp) - 1
	}
	o.TickP99US.Store(cp[idx])
}

// WriteProm writes the current metrics in Prometheus text format.
func (o *Obs) WriteProm(w io.Writer) {
	fmt.Fprintf(w, "xenomit_packets_in_total %d\n", o.PacketsIn.Load())
	fmt.Fprintf(w, "xenomit_packets_out_total %d\n", o.PacketsOut.Load())
	fmt.Fprintf(w, "xenomit_sessions %d\n", o.Sessions.Load())
	fmt.Fprintf(w, "xenomit_logins_total %d\n", o.Logins.Load())
	fmt.Fprintf(w, "xenomit_kills_total %d\n", o.Kills.Load())
	fmt.Fprintf(w, "xenomit_tick_p99_microseconds %d\n", o.TickP99US.Load())
}

// Span is one named interval. Exporter, when set, receives finished spans.
type Span struct {
	Name string
	Dur  time.Duration
}

// Exporter receives finished spans. The default is nil (no export).
var Exporter func(Span)

// Time runs fn and exports its duration.
func Time(name string, fn func()) {
	start := time.Now()
	fn()
	if Exporter != nil {
		Exporter(Span{Name: name, Dur: time.Since(start)})
	}
}
