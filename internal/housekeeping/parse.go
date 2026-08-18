package housekeeping

import (
	"fmt"
	"strconv"
	"strings"
)

var sizeUnits = map[string]int64{
	"B":   1,
	"kB":  1000,
	"KB":  1024,
	"MB":  1000 * 1000,
	"GB":  1000 * 1000 * 1000,
	"TB":  1000 * 1000 * 1000 * 1000,
	"PB":  1000 * 1000 * 1000 * 1000 * 1000,
	"KiB": 1024,
	"MiB": 1024 * 1024,
	"GiB": 1024 * 1024 * 1024,
	"TiB": 1024 * 1024 * 1024 * 1024,
}

// longest-first so "1.5MB" matches "MB", not "B"
var unitOrder = []string{"TiB", "GiB", "MiB", "KiB", "PB", "TB", "GB", "MB", "kB", "KB", "B"}

func splitNumberUnit(s string) (string, string) {
	for _, u := range unitOrder {
		if strings.HasSuffix(s, u) {
			return strings.TrimSpace(strings.TrimSuffix(s, u)), u
		}
	}
	return strings.TrimSpace(s), ""
}

func parseSize(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "0" || s == "0B" {
		return 0, nil
	}
	numStr, unit := splitNumberUnit(s)
	mult, ok := sizeUnits[unit]
	if !ok {
		num, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return 0, fmt.Errorf("parse size %q: %w", s, err)
		}
		return int64(num), nil
	}
	num, err := strconv.ParseFloat(numStr, 64)
	if err != nil {
		return 0, fmt.Errorf("parse size %q: %w", s, err)
	}
	return int64(num * float64(mult)), nil
}

func isPureInt(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func parseDfOutput(output string) (*Usage, error) {
	usage := &Usage{}
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		// The type name may span multiple words ("Build Cache", "Local Volumes");
		// it ends where the first integer column (TOTAL) begins.
		typeTokens := []string{fields[0]}
		i := 1
		for i < len(fields) && !isPureInt(fields[i]) {
			typeTokens = append(typeTokens, fields[i])
			i++
		}
		// remaining fields: TOTAL ACTIVE SIZE [RECLAIMABLE [(PCT%)]]
		rest := fields[i:]
		if len(rest) < 3 {
			continue
		}
		reclaimable := rest[len(rest)-1]
		if strings.HasPrefix(reclaimable, "(") {
			reclaimable = rest[len(rest)-2]
		}
		typ := strings.Join(typeTokens, " ")
		switch {
		case strings.Contains(typ, "Containers"):
			v, err := parseSize(reclaimable)
			if err != nil {
				return nil, err
			}
			usage.ContainersReclaimable = v
		case strings.Contains(typ, "Images"):
			v, err := parseSize(reclaimable)
			if err != nil {
				return nil, err
			}
			usage.ImagesReclaimable = v
		case strings.Contains(typ, "Volumes"):
			v, err := parseSize(reclaimable)
			if err != nil {
				return nil, err
			}
			usage.VolumesReclaimable = v
		case strings.Contains(typ, "Cache"):
			v, err := parseSize(reclaimable)
			if err != nil {
				return nil, err
			}
			usage.CacheReclaimable = v
		}
	}
	return usage, nil
}

func parseReclaimed(out string) (int64, error) {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Total reclaimed space:") {
			val := strings.TrimSpace(strings.TrimPrefix(line, "Total reclaimed space:"))
			return parseSize(val)
		}
	}
	return 0, fmt.Errorf("no 'Total reclaimed space' line in prune output")
}

func parseCandidates(out string, cat Category) []Candidate {
	var cands []Candidate
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.SplitN(line, " ", 2)
		id := fields[0]
		name := ""
		if len(fields) == 2 {
			name = fields[1]
		}
		cands = append(cands, Candidate{Category: cat, ID: id, Name: name})
	}
	return cands
}