package utils

import (
	"bufio"
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"time"
	"unsafe"

	"github.com/cheesycod/mewld/config"
	log "github.com/sirupsen/logrus"
)

func ReadLines(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines, scanner.Err()
}

// Given a config, return the directory to use
func ConfigGetDirectory(config *config.CoreConfig) (string, error) {
	var dir string
	var err error
	if config.OverrideDir != "" {
		dir = config.OverrideDir
	} else {
		var dirname string
		if config.UseCurrentDirectory {
			dirname, err = os.Getwd()

			if err != nil {
				return "", fmt.Errorf("could not find current directory: %w", err)
			}
		} else {
			dirname, err = os.UserHomeDir()
			if err != nil {
				return "", fmt.Errorf("could not find home directory: %w", err)
			}
		}

		dir = dirname + "/" + config.Dir
	}

	return dir, nil
}

func SetLogLevel() {
	lvl, ok := os.LookupEnv("LOG_LEVEL")
	// LOG_LEVEL not set, let's default to info
	if !ok {
		lvl = "info"
	}
	// parse string, this is built-in feature of logrus
	ll, err := log.ParseLevel(lvl)
	if err != nil {
		ll = log.InfoLevel
	}
	// set global log level
	log.SetLevel(ll)
}

// Given a value, return a pointer to it
func Pointer[T any](v T) *T {
	return &v
}

func SliceContains[T comparable](s []T, e T) bool {
	for _, a := range s {
		if a == e {
			return true
		}
	}
	return false
}

func SlicesEqual[T comparable](a []T, b []T) bool {
	// If lengths are unequal
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}

	return true
}

// Creates a python compatible list
func ToPyListUInt64(l []uint64) string {
	var s string = "["
	for i, v := range l {
		s += fmt.Sprint(v)
		if i != len(l)-1 {
			s += ", "
		}
	}
	return s + "]"
}

func UInt64ToString(i uint64) string {
	return strconv.FormatUint(i, 10)
}

const letterBytes = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
const (
	letterIdxBits = 6                    // 6 bits to represent a letter index
	letterIdxMask = 1<<letterIdxBits - 1 // All 1-bits, as many as letterIdxBits
	letterIdxMax  = 63 / letterIdxBits   // # of letter indices fitting in 63 bits
)

var src = rand.NewSource(time.Now().UnixNano())

func RandomString(n int) string {
	b := make([]byte, n)
	// A src.Int63() generates 63 random bits, enough for letterIdxMax characters!
	for i, cache, remain := n-1, src.Int63(), letterIdxMax; i >= 0; {
		if remain == 0 {
			cache, remain = src.Int63(), letterIdxMax
		}
		if idx := int(cache & letterIdxMask); idx < len(letterBytes) {
			b[i] = letterBytes[idx]
			i--
		}
		cache >>= letterIdxBits
		remain--
	}

	return *(*string)(unsafe.Pointer(&b))
}
