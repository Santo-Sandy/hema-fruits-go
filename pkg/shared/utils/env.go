package utils

import (
	"os"
	"strconv"
)

func GetenvStr(param string) string {
	return os.Getenv(param)
}

func GetEnvStr(param string, defaultValue string) string {
	val := os.Getenv(param)
	if val == "" {
		return defaultValue
	}
	return val
}

func GetenvInt(key string) int {
	s := GetenvStr(key)
	v, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return v
}

func GetenvBool(key string) bool {
	s := GetenvStr(key)
	v, err := strconv.ParseBool(s)
	if err != nil {
		return false
	}
	return v
}

func GetenvFloat(key string) float64 {
	s := GetenvStr(key)
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return v
}
