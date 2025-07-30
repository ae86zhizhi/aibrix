//go:build nozmq
// +build nozmq

package cache

import (
	"log"
)

func init() {
	// This will only compile with nozmq tag
	log.Println("Cache package initialized without ZMQ support")
}
