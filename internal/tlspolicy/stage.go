package tlspolicy

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// StageCertificatePair copies a read-only source pair to owner-readable files.
// Kubernetes Secret volumes are root-owned; Hikyo remains non-root and stages
// into an emptyDir so the runtime key can satisfy the exact 0400 policy.
func StageCertificatePair(certSource, keySource, destinationDir string) error {
	certPEM, err := os.ReadFile(certSource)
	if err != nil {
		return fmt.Errorf("read staged TLS certificate source: %w", err)
	}
	keyPEM, err := os.ReadFile(keySource)
	if err != nil {
		return fmt.Errorf("read staged TLS key source: %w", err)
	}
	if _, _, err := ParseCertificatePEM(certPEM, keyPEM, time.Now()); err != nil {
		return err
	}
	if err := os.MkdirAll(destinationDir, 0o700); err != nil {
		return fmt.Errorf("create staged TLS directory: %w", err)
	}
	certPath := filepath.Join(destinationDir, "tls.crt")
	keyPath := filepath.Join(destinationDir, "tls.key")
	if err := atomicWrite(certPath, certPEM, 0o444); err != nil {
		return fmt.Errorf("stage TLS certificate: %w", err)
	}
	if err := atomicWrite(keyPath, keyPEM, 0o400); err != nil {
		return fmt.Errorf("stage TLS key: %w", err)
	}
	if _, _, err := LoadCertificate(certPath, keyPath, time.Now()); err != nil {
		return fmt.Errorf("validate staged TLS pair: %w", err)
	}
	return nil
}

func atomicWrite(path string, contents []byte, mode os.FileMode) (returnErr error) {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tls-stage-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() {
		if returnErr != nil {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(contents); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

// WatchAndStageCertificatePair stages once, then polls the source files for
// Secret-volume updates until ctx is cancelled.
func WatchAndStageCertificatePair(ctx context.Context, certSource, keySource, destinationDir string, interval time.Duration) error {
	state, err := stageStableCertificatePair(certSource, keySource, destinationDir)
	if err != nil {
		return err
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			next, err := sourceState(certSource, keySource)
			if err != nil {
				return err
			}
			if next == state {
				continue
			}
			staged, err := stageStableCertificatePair(certSource, keySource, destinationDir)
			if err != nil {
				return err
			}
			state = staged
		}
	}
}

func stageStableCertificatePair(certSource, keySource, destinationDir string) (stagedSourceState, error) {
	for range 3 {
		before, err := sourceState(certSource, keySource)
		if err != nil {
			return stagedSourceState{}, err
		}
		if err := StageCertificatePair(certSource, keySource, destinationDir); err != nil {
			return stagedSourceState{}, err
		}
		after, err := sourceState(certSource, keySource)
		if err != nil {
			return stagedSourceState{}, err
		}
		if before == after {
			return after, nil
		}
	}
	return stagedSourceState{}, fmt.Errorf("TLS source changed during three consecutive staging attempts")
}

type stagedSourceState struct {
	certModTime time.Time
	certSize    int64
	keyModTime  time.Time
	keySize     int64
}

func sourceState(certPath, keyPath string) (stagedSourceState, error) {
	certInfo, err := os.Stat(certPath)
	if err != nil {
		return stagedSourceState{}, fmt.Errorf("stat staged TLS certificate source: %w", err)
	}
	keyInfo, err := os.Stat(keyPath)
	if err != nil {
		return stagedSourceState{}, fmt.Errorf("stat staged TLS key source: %w", err)
	}
	return stagedSourceState{
		certModTime: certInfo.ModTime(), certSize: certInfo.Size(),
		keyModTime: keyInfo.ModTime(), keySize: keyInfo.Size(),
	}, nil
}
