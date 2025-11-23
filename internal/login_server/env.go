package login_server

import (
	"os"
	"strconv"
)

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getenvInt(key string, def int64) int64 {
	raw := getenv(key, "")
	if raw == "" {
		return def
	}
	if v, err := strconv.ParseInt(raw, 10, 64); err == nil {
		return v
	}
	return def
}

func getenvUint32(key string, def uint32) uint32 {
	raw := getenv(key, "")
	if raw == "" {
		return def
	}
	if v, err := strconv.ParseUint(raw, 10, 32); err == nil {
		return uint32(v)
	}
	return def
}
