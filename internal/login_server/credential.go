package login_server

import (
	"crypto/rand"
	"encoding/base64"
)

func generateCredential() string {
	buf := make([]byte, 32)
	_, _ = rand.Read(buf)
	return base64.RawURLEncoding.EncodeToString(buf)
}
