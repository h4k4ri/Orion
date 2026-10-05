package config

import (
	"os"
	"strconv"
	"time"
)

func String(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}

	return fallback
}

func Int(key string, fallback int) int {
	if value, ok := os.LookupEnv(key); ok {
		parsed, err := strconv.Atoi(value)
		if err == nil {
			return parsed
		}
	}

	return fallback
}

func Duration(key string, fallback time.Duration) time.Duration {
	if value, ok := os.LookupEnv(key); ok {
		parsed, err := time.ParseDuration(value)
		if err == nil {
			return parsed
		}
	}

	return fallback
}

func PositiveDuration(key string, fallback time.Duration) time.Duration {
	value := Duration(key, fallback)
	if value <= 0 {
		return fallback
	}
	return value
}
