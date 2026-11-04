// Command fipsclient is a minimal TLS relying party for TestMLDSARelyingPartyWithoutMLDSARefuses.
// Built with GOFIPS140=v1.0.0 it uses the Go Cryptographic Module v1.0.0, which has no ML-DSA, so it
// stands in for a maintained relying party that predates post-quantum signatures.
package main

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"os"
)

func main() {
	if len(os.Args) != 4 {
		fmt.Fprintln(os.Stderr, "usage: fipsclient ADDR SERVERNAME ROOT.pem")
		os.Exit(2)
	}
	rootPEM, err := os.ReadFile(os.Args[3])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(rootPEM)
	conn, err := tls.Dial("tcp", os.Args[1], &tls.Config{RootCAs: roots, ServerName: os.Args[2], MinVersion: tls.VersionTLS13})
	if err != nil {
		fmt.Println("REFUSED:", err)
		os.Exit(1)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte("ping")); err != nil {
		fmt.Println("REFUSED:", err)
		os.Exit(1)
	}
	reply := make([]byte, 4)
	if _, err := io.ReadFull(conn, reply); err != nil {
		fmt.Println("REFUSED:", err)
		os.Exit(1)
	}
	fmt.Println("ACCEPTED:", string(reply))
}
