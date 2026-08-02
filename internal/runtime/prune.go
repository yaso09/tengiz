package runtime

import (
	"strconv"
	"strings"
)

var sizeUnits = []struct {
	suffix string
	mult   int64
}{
	{"TB", 1_000_000_000_000},
	{"GB", 1_000_000_000},
	{"MB", 1_000_000},
	{"kB", 1_000},
	{"B", 1},
}

func parseSize(s string) int64 {
	s = strings.TrimSpace(s)
	for _, u := range sizeUnits {
		if strings.HasSuffix(s, u.suffix) {
			numStr := strings.TrimSuffix(s, u.suffix)
			num, err := strconv.ParseFloat(numStr, 64)
			if err != nil {
				return 0
			}
			return int64(num * float64(u.mult))
		}
	}
	return 0
}

func parseReclaimedSpace(output string) int64 {
	var total int64
	for _, line := range strings.Split(output, "\n") {
		const marker = "Total reclaimed space:"
		idx := strings.Index(line, marker)
		if idx < 0 {
			continue
		}
		val := strings.TrimSpace(line[idx+len(marker):])
		total += parseSize(val)
	}
	return total
}

func FormatBytes(n int64) string {
	if n < 1_000 {
		return strconv.FormatInt(n, 10) + "B"
	}
	for _, u := range sizeUnits {
		if n >= u.mult {
			return strconv.FormatFloat(float64(n)/float64(u.mult), 'f', 2, 64) + u.suffix
		}
	}
	return strconv.FormatInt(n, 10) + "B"
}
