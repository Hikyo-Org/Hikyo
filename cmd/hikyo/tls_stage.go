package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/tlspolicy"
)

const tlsStageMode = "__hikyo-stage-tls"

func runTLSStageMode(args []string) (bool, int) {
	if len(args) == 0 || args[0] != tlsStageMode {
		return false, 0
	}
	if len(args) > 2 || (len(args) == 2 && args[1] != "--once") {
		fmt.Fprintln(os.Stderr, "hikyo internal TLS stage: expected only --once")
		return true, 2
	}
	const source = "/run/hikyo-tls-source"
	const destination = "/run/hikyo-tls"
	if len(args) == 2 {
		if err := tlspolicy.StageCertificatePair(source+"/tls.crt", source+"/tls.key", destination); err != nil {
			fmt.Fprintln(os.Stderr, "hikyo internal TLS stage:", err)
			return true, 1
		}
		return true, 0
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := tlspolicy.WatchAndStageCertificatePair(ctx, source+"/tls.crt", source+"/tls.key", destination, 5*time.Second); err != nil {
		fmt.Fprintln(os.Stderr, "hikyo internal TLS stage:", err)
		return true, 1
	}
	return true, 0
}
