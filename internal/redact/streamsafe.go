package redact

import "bytes"

// placeholderStartBytes is the fixed byte prefix that begins every
// placeholder token (the UTF-8 bytes of "⟦RG:").
var placeholderStartBytes = []byte(placeholderOpen)

// SafeCutPoint returns the number of leading bytes of buf that are safe to
// restore and emit immediately when processing a response stream
// incrementally: everything through the end of the last complete
// placeholder token, plus any trailing bytes that cannot possibly be the
// start of another placeholder token. The remaining bytes (buf[cut:]) should
// be held back and prefixed onto the next chunk.
func SafeCutPoint(buf []byte) int {
	safeEnd := 0
	if locs := placeholderRe.FindAllIndex(buf, -1); len(locs) > 0 {
		safeEnd = locs[len(locs)-1][1]
	}
	return safeEnd + safePrefixLen(buf[safeEnd:])
}

// safePrefixLen returns the length of the leading portion of tail that
// contains no byte sequence which could be (a prefix of) placeholderStartBytes.
func safePrefixLen(tail []byte) int {
	for i := range tail {
		n := len(placeholderStartBytes)
		if rem := len(tail) - i; rem < n {
			n = rem
		}
		if bytes.Equal(tail[i:i+n], placeholderStartBytes[:n]) {
			return i
		}
	}
	return len(tail)
}
