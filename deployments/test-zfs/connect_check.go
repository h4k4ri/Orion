//go:build connectivity

package main

import (
	"fmt"
	"net"
	"time"
)

func main() {
	fmt.Println("=== Testing ZFS Plugin Connection ===")

	// Test TCP connection
	conn, err := net.DialTimeout("tcp", "localhost:50051", 5*time.Second)
	if err != nil {
		fmt.Printf("FAILED: Cannot connect to ZFS plugin: %v\n", err)
		return
	}
	defer conn.Close()

	fmt.Println("SUCCESS: Connected to ZFS plugin on :50051")

	// Try to read some data (gRPC sends headers immediately)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	if err != nil && err.Error() != "EOF" {
		fmt.Printf("Read error (expected for gRPC): %v\n", err)
	} else {
		fmt.Printf("Received %d bytes from ZFS plugin (gRPC headers)\n", n)
	}

	fmt.Println("\n=== Testing PostgreSQL Connection ===")

	pgConn, err := net.DialTimeout("tcp", "localhost:5432", 5*time.Second)
	if err != nil {
		fmt.Printf("FAILED: Cannot connect to PostgreSQL: %v\n", err)
		return
	}
	defer pgConn.Close()

	fmt.Println("SUCCESS: Connected to PostgreSQL on :5432")

	fmt.Println("\n=== All connectivity tests passed! ===")
}
