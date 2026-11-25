package pcremote

import "crypto/sha1"

// sha1Sum is for hosts older than version 7, which pair with SHA-1.
func sha1Sum(b []byte) [20]byte { return sha1.Sum(b) }
