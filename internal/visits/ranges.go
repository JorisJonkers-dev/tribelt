package visits

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/netip"
	"path"
	"strings"
)

//go:embed ranges/*.json
var bundledRanges embed.FS

// Ranges holds the published crawler address ranges per operator.
type Ranges struct {
	byOperator map[string][]netip.Prefix
}

// BundledRanges loads the ranges shipped with the binary (refresh with `task update-ranges`).
func BundledRanges() (*Ranges, error) {
	sub, _ := fs.Sub(bundledRanges, "ranges")
	return LoadRanges(sub)
}

type rangeFile struct {
	Prefixes []struct {
		IPv4 string `json:"ipv4Prefix"`
		IPv6 string `json:"ipv6Prefix"`
	} `json:"prefixes"`
}

// LoadRanges reads every "<operator>-<list>.json" file in the root of fsys.
func LoadRanges(fsys fs.FS) (*Ranges, error) {
	files, _ := fs.Glob(fsys, "*.json") // the pattern is constant; Glob ignores I/O errors
	r := &Ranges{byOperator: map[string][]netip.Prefix{}}
	for _, name := range files {
		raw, err := fs.ReadFile(fsys, name)
		if err != nil {
			return nil, err
		}
		var f rangeFile
		if err := json.Unmarshal(raw, &f); err != nil {
			return nil, fmt.Errorf("ranges %s: %w", name, err)
		}
		operator, _, _ := strings.Cut(strings.TrimSuffix(path.Base(name), ".json"), "-")
		for _, p := range f.Prefixes {
			pfx, err := netip.ParsePrefix(p.IPv4 + p.IPv6)
			if err != nil {
				return nil, fmt.Errorf("ranges %s: %w", name, err)
			}
			r.byOperator[operator] = append(r.byOperator[operator], pfx.Masked())
		}
	}
	return r, nil
}

// Operators lists the operators with at least one range.
func (r *Ranges) Operators() []string {
	out := make([]string, 0, len(r.byOperator))
	for op := range r.byOperator {
		out = append(out, op)
	}
	return out
}

// Contains reports whether ip lies in one of the operator's published ranges.
func (r *Ranges) Contains(operator string, ip netip.Addr) bool {
	if r == nil || !ip.IsValid() {
		return false
	}
	ip = ip.Unmap()
	for _, p := range r.byOperator[operator] {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}
