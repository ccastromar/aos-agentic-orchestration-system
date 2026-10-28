package metrics

import (
	"fmt"
	"io"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// processStart is when AOS started: process_start_time_seconds.
var processStart = time.Now()

// writeRuntime writes the Go runtime and process metrics (the USE side of AOS: saturation of
// memory, goroutines, CPU and file descriptors) under the names Prometheus's own Go client uses,
// read at scrape time. RSS and file descriptors come from /proc, so only on Linux (the
// containers); elsewhere they are left out.
func writeRuntime(w io.Writer, openMetrics bool) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	gauge := func(name, help string, v float64) {
		fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s gauge\n%s %g\n", name, help, name, name, v)
	}
	counter := func(name, help string, v float64) {
		family := name
		if openMetrics {
			family = strings.TrimSuffix(name, "_total")
		}
		fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s counter\n%s %g\n", family, help, family, name, v)
	}

	gauge("go_goroutines", "Number of goroutines that currently exist.", float64(runtime.NumGoroutine()))
	gauge("go_memstats_heap_alloc_bytes", "Bytes of allocated heap objects.", float64(m.HeapAlloc))
	gauge("go_memstats_heap_inuse_bytes", "Bytes in in-use heap spans.", float64(m.HeapInuse))
	gauge("go_memstats_sys_bytes", "Bytes of memory obtained from the OS.", float64(m.Sys))
	counter("go_gc_cycles_total", "Completed GC cycles.", float64(m.NumGC))
	counter("go_gc_pause_seconds_total", "Total time the world was stopped for GC.", float64(m.PauseTotalNs)/1e9)

	if cpu, ok := cpuSeconds(); ok {
		counter("process_cpu_seconds_total", "Total user and system CPU time spent in seconds.", cpu)
	}
	gauge("process_start_time_seconds", "Start time of the process since unix epoch in seconds.", float64(processStart.UnixMilli())/1000)

	if rss, ok := residentBytes(); ok {
		gauge("process_resident_memory_bytes", "Resident memory size in bytes.", rss)
	}
	if fds, err := os.ReadDir("/proc/self/fd"); err == nil {
		gauge("process_open_fds", "Number of open file descriptors.", float64(len(fds)))
		if max, ok := maxFDs(); ok {
			gauge("process_max_fds", "Maximum number of open file descriptors.", max)
		}
	}
}

// residentBytes reads the resident set size from /proc/self/statm (pages), on Linux.
func residentBytes() (float64, bool) {
	raw, err := os.ReadFile("/proc/self/statm")
	if err != nil {
		return 0, false
	}
	fields := strings.Fields(string(raw))
	if len(fields) < 2 {
		return 0, false
	}
	pages, err := strconv.ParseFloat(fields[1], 64)
	if err != nil {
		return 0, false
	}
	return pages * float64(os.Getpagesize()), true
}
