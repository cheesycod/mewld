// Core utilities to break import cycles
package coreutils

import (
	"fmt"
	"log"
	"strconv"
)

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

func ParseUint64(s string) uint64 {
	i, err := strconv.ParseUint(s, 10, 64)

	if err != nil {
		log.Fatal(err)
	}

	return i
}

func UInt64ToString(i uint64) string {
	return strconv.FormatUint(i, 10)
}
