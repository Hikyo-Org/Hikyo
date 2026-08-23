// Command oidctest-idp exposes the internal fake provider to browser-flow tests.
// It is intentionally absent from release builds and .goreleaser.yaml.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/Hikyo-Org/hikyo/internal/oidctest"
)

type redirectFlags []string

func (r *redirectFlags) String() string { return strings.Join(*r, ",") }
func (r *redirectFlags) Set(value string) error {
	*r = append(*r, value)
	return nil
}

func main() {
	listen := flag.String("listen", "127.0.0.1:0", "listener address")
	authTimeSkew := flag.Duration("auth-time-skew", 0, "age applied to auth_time at token mint")
	amr := flag.String("amr", "mfa,otp", "comma-separated AMR values")
	var redirects redirectFlags
	flag.Var(&redirects, "redirect-uri", "exact registered callback URI; repeatable")
	flag.Parse()

	idp, err := oidctest.NewAt(*listen)
	if err != nil {
		fatal(err)
	}
	defer idp.Close()
	for _, redirect := range redirects {
		if err := idp.RegisterRedirectURI(redirect); err != nil {
			fatal(err)
		}
	}
	idp.AuthTimeNow = true
	idp.AuthTimeSkew = *authTimeSkew
	for _, value := range strings.Split(*amr, ",") {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			idp.AMR = append(idp.AMR, trimmed)
		}
	}

	fmt.Println(idp.Issuer())
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt, syscall.SIGTERM)
	<-interrupt
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
