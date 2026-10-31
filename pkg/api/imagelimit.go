package api

import (
	"regexp"
	"strconv"
)

var manyImageLimit = regexp.MustCompile(`many-image requests:\s*(\d+)\s*pixels`)

// ManyImageLimit reports the per-image pixel limit a 400 names when a request
// held more than 20 images and one of them was over the stricter limit that
// then applies ("... exceed max allowed size for many-image requests: 2000
// pixels"). The limit differs by platform and has changed over time, so the
// caller takes it from the error rather than from a constant.
func ManyImageLimit(err error) (int, bool) {
	if err == nil {
		return 0, false
	}
	m := manyImageLimit.FindStringSubmatch(errDetail(err))
	if m == nil {
		return 0, false
	}
	n, convErr := strconv.Atoi(m[1])
	return n, convErr == nil && n > 0
}
