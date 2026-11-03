package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/Hikyo-Org/hikyo/internal/configrollout"
	"github.com/Hikyo-Org/hikyo/internal/multicall"
	"github.com/Hikyo-Org/hikyo/internal/securefile"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

func runConfigRollout(ctx context.Context, args []string, stderr io.Writer) int {
	fs := flag.NewFlagSet("config-rollout", flag.ContinueOnError)
	fs.SetOutput(stderr)
	enrollmentPath := fs.String("enrollment-file", "/run/hikyo/rollout/enrollment/enrollment.json", "operator-installed rollout enrollment")
	publicPath := fs.String("authority-public-key", "/run/hikyo/rollout/enrollment/authority.pub", "operator-installed deployment authority public keys: Ed25519, or Ed25519 and ML-DSA-65 as consecutive PUBLIC KEY blocks")
	printIDs := fs.Bool("print-authority-ids", false, "print the algorithm and key id of each key in --authority-public-key and exit; contacts no cluster")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if *printIDs {
		return printAuthorityIDs(*publicPath, stderr)
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
	keys, err := configrollout.ParsePublicKeys(publicRaw)
	if err != nil {
		fmt.Fprintln(stderr, "hikyo config-rollout: invalid authority public key")
		return 1
	}
	// The held keys must be exactly the enrolled ones: a stale or swapped key
	// file never becomes the trust anchor.
	if err := configrollout.CheckKeys(enrollment, keys); err != nil {
		fmt.Fprintln(stderr, "hikyo config-rollout: authority public key does not match the enrollment")
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
	controller, err := configrollout.NewControllerKeys(client, enrollment, keys)
	if err != nil {
		fmt.Fprintln(stderr, "hikyo config-rollout: invalid installed target")
		return 1
	}
	if err = controller.Run(ctx, types.UID(os.Getenv("POD_UID"))); err != nil && !errors.Is(err, context.Canceled) {
		if errors.Is(err, configrollout.ErrTrust) {
			fmt.Fprintln(stderr, "hikyo config-rollout: authority trust state refused; see the enrollment runbook")
			return 1
		}
		fmt.Fprintln(stderr, "hikyo config-rollout: executor custody unavailable")
		return 1
	}
	return 0
}

func printAuthorityIDs(path string, stderr io.Writer) int {
	raw, err := readRolloutInstalled(path)
	if err != nil {
		fmt.Fprintln(stderr, "hikyo config-rollout: authority public key unavailable")
		return 1
	}
	keys, err := configrollout.ParsePublicKeys(raw)
	if err != nil {
		fmt.Fprintln(stderr, "hikyo config-rollout: invalid authority public key")
		return 1
	}
	ids := keys.KeyIDs()
	for _, algorithm := range []string{configrollout.AlgEd25519, configrollout.AlgMLDSA65} {
		if id, ok := ids[algorithm]; ok {
			fmt.Println(algorithm, id)
		}
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
	if len(args) == 0 || args[0] != multicall.RolloutAuthorityStage {
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
	// One Ed25519 key (compatibility) or an Ed25519 and a seed-only ML-DSA-65
	// key (strict); anything else is refused before it reaches the volume the
	// server reads.
	signers, err := configrollout.ParseSigners(raw)
	if err != nil || signers.Ed25519 == nil && signers.MLDSA65 == nil {
		return errors.New("invalid authority encoding")
	}
	return securefile.WriteAtomic(destination, raw, 0400)
}
