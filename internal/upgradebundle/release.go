package upgradebundle

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
)

const maxBundlePayloadBytes int64 = 8 << 30

func (r *documentReader) release(snapshot releasetrust.Snapshot, entry ReleaseEntry) (releasetrust.VerifiedRelease, []byte, error) {
	dir := "releases/" + string(entry.ManifestSHA256) + "/"
	manifest, err := r.read(dir + "manifest.json")
	if err != nil {
		return releasetrust.VerifiedRelease{}, nil, err
	}
	if releaseidentity.Hash(manifest) != entry.ManifestSHA256 {
		return releasetrust.VerifiedRelease{}, nil, errors.New("offline manifest directory digest mismatch")
	}
	signature, err := r.read(dir + "manifest.sigstore.json")
	if err != nil {
		return releasetrust.VerifiedRelease{}, nil, err
	}
	compatibility, err := r.read(dir + "upgrade-compatibility.json")
	if err != nil {
		return releasetrust.VerifiedRelease{}, nil, err
	}
	var release releasetrust.VerifiedRelease
	switch entry.Profile {
	case releaseidentity.StableV1:
		candidate, err := r.read(dir + "release-candidate.json")
		if err != nil {
			return releasetrust.VerifiedRelease{}, nil, err
		}
		release, err = releasetrust.VerifyStable(snapshot, releasetrust.StableMaterial{Manifest: manifest, ManifestSignature: signature, Candidate: candidate, Compatibility: compatibility})
		if err != nil {
			return releasetrust.VerifiedRelease{}, nil, fmt.Errorf("authenticate offline stable release: %w", err)
		}
	case releaseidentity.NightlyV1:
		policy, err := r.read(dir + "policy.json")
		if err != nil {
			return releasetrust.VerifiedRelease{}, nil, err
		}
		trustedRoot, err := r.read(dir + "trusted-root.json")
		if err != nil {
			return releasetrust.VerifiedRelease{}, nil, err
		}
		assets, closers, err := r.payloads(dir + "payloads")
		if err != nil {
			return releasetrust.VerifiedRelease{}, nil, err
		}
		defer func() {
			for _, closer := range closers {
				closer.Close()
			}
		}()
		release, err = releasetrust.VerifyNightly(snapshot, releasetrust.NightlyMaterial{Manifest: manifest, Bundle: signature, Policy: policy, TrustedRoot: trustedRoot, Compatibility: compatibility, Artifacts: assets})
		if err != nil {
			return releasetrust.VerifiedRelease{}, nil, fmt.Errorf("authenticate offline nightly release: %w", err)
		}
	default:
		return releasetrust.VerifiedRelease{}, nil, errors.New("unknown offline release profile")
	}
	return release, compatibility, nil
}

func (r *documentReader) payloads(directory string) (map[string]io.Reader, []*payloadReader, error) {
	if err := r.ctx.Err(); err != nil {
		return nil, nil, err
	}
	dir, err := openDirectory(r.root, directory)
	if err != nil {
		return nil, nil, errors.New("nightly payload directory unavailable")
	}
	defer dir.Close()
	if info, err := dir.Stat(); err != nil || !info.IsDir() {
		return nil, nil, errors.New("nightly payload inventory requires directory")
	}
	entries, err := dir.ReadDir(releasetrust.MaxArtifacts + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, nil, err
	}
	if len(entries) == 0 || len(entries) > releasetrust.MaxArtifacts {
		return nil, nil, errors.New("nightly payload directory exceeds inventory bound")
	}
	assets := map[string]io.Reader{}
	closers := []*payloadReader{}
	for _, entry := range entries {
		if !entry.Type().IsRegular() {
			return nil, nil, errors.New("nightly payload must be a regular file")
		}
		reader := &payloadReader{ctx: r.ctx, root: r.root, name: directory + "/" + entry.Name(), aggregate: &r.payloadBytes}
		assets[entry.Name()] = reader
		closers = append(closers, reader)
	}
	return assets, closers, nil
}

type payloadReader struct {
	ctx       context.Context
	root      *os.Root
	name      string
	file      *os.File
	read      int64
	aggregate *int64
	done      bool
}

func (r *payloadReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	if r.done {
		return 0, io.EOF
	}
	if r.file == nil {
		file, err := openDocument(r.root, r.name)
		if err != nil {
			return 0, errors.New("nightly payload unavailable")
		}
		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() || info.Size() > releasetrust.MaxArtifactBytes {
			file.Close()
			return 0, errors.New("nightly payload exceeds regular-file bound")
		}
		r.file = file
	}
	n, err := r.file.Read(p)
	r.read += int64(n)
	*r.aggregate += int64(n)
	if r.read > releasetrust.MaxArtifactBytes || *r.aggregate > maxBundlePayloadBytes {
		r.Close()
		return 0, errors.New("offline payload bytes exceed bound")
	}
	if err != nil {
		r.Close()
	}
	return n, err
}
func (r *payloadReader) Close() error {
	r.done = true
	if r.file != nil {
		return r.file.Close()
	}
	return nil
}
