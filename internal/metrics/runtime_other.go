//go:build !unix

package metrics

// Outside Unix (Windows builds) the process CPU and file-descriptor metrics are left out.
func cpuSeconds() (float64, bool) { return 0, false }

func maxFDs() (float64, bool) { return 0, false }
