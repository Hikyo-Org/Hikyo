package main

import (
	"context"
	"crypto/ed25519"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/Hikyo-Org/hikyo/internal/configrollout"
	"github.com/Hikyo-Org/hikyo/internal/securefile"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

func runConfigRollout(ctx context.Context, args []string, stderr io.Writer) int {
	fs := flag.NewFlagSet("config-rollout", flag.ContinueOnError)
	fs.SetOutput(stderr)
	enrollmentPath := fs.String("enrollment-file", "/run/hikyo/rollout/enrollment/enrollment.json", "operator-installed rollout enrollment")
	publicPath := fs.String("authority-public-key", "/run/hikyo/rollout/enrollment/authority.pub", "operator-installed Ed25519 deployment authority public key")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return 2
	}
	enrollmentRaw, err := readRolloutInstalled(*enrollmentPath)
	if err != nil {
		fmt.Fprintln(stderr, "hikyo config-rollout: enrollment unavailable")
		return 1
	}
	enrollment, err := configrollout.ParseEnrollment(enrollmentRaw)
	if err != nil {
		fmt.Fprintln(stderr, "hikyo config-rollout: invalid enrollment")
		return 1
	}
	publicRaw, err := readRolloutInstalled(*publicPath)
	if err != nil {
		fmt.Fprintln(stderr, "hikyo config-rollout: authority public key unavailable")
		return 1
	}
	block, trailing := pem.Decode(publicRaw)
	if block == nil || block.Type != "PUBLIC KEY" || len(trailing) != 0 {
		fmt.Fprintln(stderr, "hikyo config-rollout: invalid authority public key")
		return 1
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	public, ok := parsed.(ed25519.PublicKey)
	if err != nil || !ok {
		fmt.Fprintln(stderr, "hikyo config-rollout: authority must use Ed25519")
		return 1
	}
	cluster, err := rest.InClusterConfig()
	if err != nil {
		fmt.Fprintln(stderr, "hikyo config-rollout: installed cluster identity unavailable")
		return 1
	}
	cluster.QPS = 10
	cluster.Burst = 20
	client, err := kubernetes.NewForConfig(cluster)
	if err != nil {
		fmt.Fprintln(stderr, "hikyo config-rollout: cluster client unavailable")
		return 1
	}
	controller, err := configrollout.NewController(client, enrollment, public)
	if err != nil {
		fmt.Fprintln(stderr, "hikyo config-rollout: invalid installed target")
		return 1
	}
	if err = controller.Run(ctx, types.UID(os.Getenv("POD_UID"))); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(stderr, "hikyo config-rollout: executor custody unavailable")
		return 1
	}
	return 0
}

func readRolloutInstalled(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, 65537))
	if err != nil || len(raw) > 65536 {
		return nil, errors.New("invalid installed rollout input")
	}
	return raw, nil
}

func runRolloutAuthorityStage(args []string) (bool, int) {
	if len(args) == 0 || args[0] != "__hikyo-stage-rollout-authority" {
		return false, 0
	}
	if len(args) != 1 {
		return true, 2
	}
	if err := stageRolloutAuthority("/run/hikyo/rollout/authority-source/authority.key", "/run/hikyo/rollout/authority/authority.key"); err != nil {
		fmt.Fprintln(os.Stderr, "hikyo config-rollout: invalid deployment authority source")
		return true, 1
	}
	return true, 0
}

func stageRolloutAuthority(source, destination string) error {
	raw, err := readRolloutInstalled(source)
	if err != nil {
		return err
	}
	defer clear(raw)
	block, trailing := pem.Decode(raw)
	if block == nil || block.Type != "PRIVATE KEY" || len(trailing) != 0 {
		return errors.New("invalid authority encoding")
	}
	defer clear(block.Bytes)
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return errors.New("invalid authority encoding")
	}
	private, ok := parsed.(ed25519.PrivateKey)
	if !ok {
		return errors.New("invalid authority algorithm")
	}
	defer clear(private)
	return securefile.WriteAtomic(destination, raw, 0400)
}
