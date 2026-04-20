package main

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"runtime/pprof"
	"runtime/trace"
	"strconv"
	"strings"
	"time"
)

// registerPProfHandlers wires Go pprof endpoints onto the given mux using the
// runtime/pprof and runtime/trace packages directly. Importing net/http/pprof
// would be shorter, but its package init() registers the same paths on
// http.DefaultServeMux as a side effect — the metrics spec forbids that
// pollution so we hand-roll the handlers here.
//
// Access control is the caller's responsibility: these handlers have no
// authentication and should only be reachable over a trusted network.
func registerPProfHandlers(mux *http.ServeMux) {
	mux.HandleFunc("/debug/pprof/", pprofIndex)
	mux.HandleFunc("/debug/pprof/cmdline", pprofCmdline)
	mux.HandleFunc("/debug/pprof/profile", pprofCPU)
	mux.HandleFunc("/debug/pprof/symbol", pprofSymbol)
	mux.HandleFunc("/debug/pprof/trace", pprofTrace)
	for _, name := range []string{"heap", "goroutine", "allocs", "threadcreate", "block", "mutex"} {
		mux.HandleFunc("/debug/pprof/"+name, func(w http.ResponseWriter, r *http.Request) {
			pprofNamed(w, r, name)
		})
	}
}

func pprofIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/debug/pprof/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(`<html>
<head><title>/debug/pprof/</title></head>
<body>
<h1>/debug/pprof/</h1>
<p>Types of profiles available:</p>
<ul>
<li><a href="heap?debug=1">heap</a></li>
<li><a href="goroutine?debug=1">goroutine</a></li>
<li><a href="allocs?debug=1">allocs</a></li>
<li><a href="threadcreate?debug=1">threadcreate</a></li>
<li><a href="block?debug=1">block</a></li>
<li><a href="mutex?debug=1">mutex</a></li>
<li><a href="profile?seconds=30">profile</a> (CPU, 30s default)</li>
<li><a href="trace?seconds=1">trace</a> (execution tracer)</li>
<li><a href="cmdline">cmdline</a></li>
</ul>
</body>
</html>`))
}

func pprofCmdline(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(strings.Join(os.Args, "\x00")))
}

func pprofCPU(w http.ResponseWriter, r *http.Request) {
	seconds := querySeconds(r, 30)
	// Start first; set download headers only after the profiler is running so
	// a failure doesn't leave stale Content-Disposition on a 500 response.
	if err := pprof.StartCPUProfile(w); err != nil {
		http.Error(w, "could not start CPU profile: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="profile"`)
	sleepInterruptible(r, seconds)
	pprof.StopCPUProfile()
}

func pprofTrace(w http.ResponseWriter, r *http.Request) {
	seconds := querySeconds(r, 1)
	if err := trace.Start(w); err != nil {
		http.Error(w, "could not start trace: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="trace"`)
	sleepInterruptible(r, seconds)
	trace.Stop()
}

// pprofSymbol resolves program counter addresses passed as `0xHEX+0xHEX...`
// (GET query "?0x…+0x…" or POST body) to function names using
// runtime.FuncForPC. Output format matches net/http/pprof.Symbol so that
// `go tool pprof` can consume it transparently.
func pprofSymbol(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")

	var reader io.Reader
	if r.Method == http.MethodPost {
		reader = r.Body
	} else {
		reader = strings.NewReader(r.URL.RawQuery)
	}

	data, err := io.ReadAll(reader)
	if err != nil {
		_, _ = fmt.Fprintf(w, "read error: %v\n", err)
		return
	}

	tokens := strings.FieldsFunc(string(data), func(r rune) bool {
		return r == '+' || r == ' '
	})

	var resolved []string
	for _, tok := range tokens {
		pc, err := strconv.ParseUint(strings.TrimPrefix(tok, "0x"), 16, 64)
		if err != nil {
			continue
		}
		fn := runtime.FuncForPC(uintptr(pc))
		if fn == nil {
			continue
		}
		resolved = append(resolved, fmt.Sprintf("%#x %s", pc, fn.Name()))
	}

	bw := bufio.NewWriter(w)
	_, _ = fmt.Fprintf(bw, "num_symbols: %d\n", len(resolved))
	for _, line := range resolved {
		_, _ = fmt.Fprintln(bw, line)
	}
	_ = bw.Flush()
}

func pprofNamed(w http.ResponseWriter, r *http.Request, name string) {
	p := pprof.Lookup(name)
	if p == nil {
		http.Error(w, fmt.Sprintf("unknown profile %q", name), http.StatusNotFound)
		return
	}
	debug := 0
	if v := r.URL.Query().Get("debug"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			debug = n
		}
	}
	if name == "heap" && r.URL.Query().Get("gc") != "" {
		runtime.GC()
	}
	if debug != 0 {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	} else {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	}
	_ = p.WriteTo(w, debug)
}

func querySeconds(r *http.Request, def int) int {
	v := r.URL.Query().Get("seconds")
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return def
	}
	return n
}

func sleepInterruptible(r *http.Request, seconds int) {
	if seconds <= 0 {
		return
	}
	select {
	case <-time.After(time.Duration(seconds) * time.Second):
	case <-r.Context().Done():
	}
}
