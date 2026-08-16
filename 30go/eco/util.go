package main

import (
	"crypto/rand"
	"fmt"
)

func randSuffix() string {
	b := make([]byte, 2)
	rand.Read(b)
	return fmt.Sprintf("%d", int(b[0])%90+10)
}
