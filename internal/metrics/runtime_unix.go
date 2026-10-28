//go:build unix

package metrics

import "syscall"

// cpuSeconds is the user and system CPU time the process has used.
func cpuSeconds() (float64, bool) {
	var ru syscall.Rusage
	if syscall.Getrusage(syscall.RUSAGE_SELF, &ru) != nil {
		return 0, false
	}
	return float64(ru.Utime.Sec+ru.Stime.Sec) + float64(ru.Utime.Usec+ru.Stime.Usec)/1e6, true
}

// maxFDs is the soft limit on open file descriptors.
func maxFDs() (float64, bool) {
	var lim syscall.Rlimit
	if syscall.Getrlimit(syscall.RLIMIT_NOFILE, &lim) != nil {
		return 0, false
	}
	return float64(lim.Cur), true
}
