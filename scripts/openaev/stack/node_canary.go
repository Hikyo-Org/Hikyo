// node_canary listens only on the synthetic metadata address inside the node.
// It supplies a real positive TCP control, without serving any host state.
package main

import (
	"net"
	"os"
)

func main() {
	listener, err := net.Listen("tcp4", "169.254.169.254:80")
	if err != nil {
		os.Exit(1)
	}
	defer func() { _ = listener.Close() }()
	for {
		connection, err := listener.Accept()
		if err != nil {
			os.Exit(1)
		}
		_ = connection.Close()
	}
}
