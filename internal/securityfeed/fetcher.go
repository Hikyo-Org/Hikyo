package securityfeed

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/theupdateframework/go-tuf/v2/metadata"
)

var metadataName = regexp.MustCompile(`^(?:[1-9][0-9]*\.)?(?:root|timestamp|snapshot|targets)\.json$`)
var catalogName = regexp.MustCompile(`^security/[0-9a-f]{64}\.catalog\.json$`)

type boundedFetcher struct {
	ctx                     context.Context
	client                  *http.Client
	metadataURL, targetsURL string
	local                   *os.Root
	requests                int
	bytes                   int64
	floors                  map[string]roleFloor
}

func (f *boundedFetcher) DownloadFile(rawURL string, max int64, _ time.Duration) ([]byte, error) {
	if err := f.ctx.Err(); err != nil {
		return nil, err
	}
	f.requests++
	if f.requests > maxRequests || max <= 0 || max > MaxDocumentBytes || f.bytes > maxAggregateBytes {
		return nil, errors.New("security feed download exceeds request or byte budget")
	}
	var relative, role string
	if strings.HasPrefix(rawURL, f.metadataURL) && metadataName.MatchString(strings.TrimPrefix(rawURL, f.metadataURL)) {
		relative = path.Join("metadata", strings.TrimPrefix(rawURL, f.metadataURL))
		name := strings.TrimSuffix(path.Base(relative), ".json")
		_, role, _ = strings.Cut(name, ".")
		if role == "" {
			role = name
		}
	} else if strings.HasPrefix(rawURL, f.targetsURL) && catalogName.MatchString(strings.TrimPrefix(rawURL, f.targetsURL)) {
		relative = path.Join("targets", strings.TrimPrefix(rawURL, f.targetsURL))
	} else {
		return nil, errors.New("security feed requested an unauthorized origin or path")
	}
	var reader io.ReadCloser
	if f.local != nil {
		info, err := f.local.Lstat(relative)
		if errors.Is(err, os.ErrNotExist) {
			return nil, &metadata.ErrDownloadHTTP{StatusCode: http.StatusNotFound, URL: rawURL}
		}
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() > max {
			return nil, errors.New("offline security feed file is not a bounded regular file")
		}
		reader, err = f.local.Open(relative)
		if err != nil {
			return nil, err
		}
	} else {
		request, err := http.NewRequestWithContext(f.ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return nil, err
		}
		request.Header.Set("User-Agent", "Hikyo-Security-Maintenance/1")
		request.Header.Set("Accept", "application/json")
		request.Header.Set("Cache-Control", "no-cache")
		request.Close = true
		response, err := f.client.Do(request)
		if err != nil {
			return nil, err
		}
		if response.StatusCode != http.StatusOK {
			response.Body.Close()
			return nil, &metadata.ErrDownloadHTTP{StatusCode: response.StatusCode, URL: rawURL}
		}
		if response.ContentLength > max {
			response.Body.Close()
			return nil, errors.New("security feed Content-Length exceeds byte budget")
		}
		reader = response.Body
	}
	defer reader.Close()
	remaining := int64(maxAggregateBytes) - f.bytes
	if max > remaining {
		max = remaining
	}
	raw, err := io.ReadAll(io.LimitReader(reader, max+1))
	f.bytes += int64(len(raw))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > max {
		return nil, fmt.Errorf("security feed response exceeds %d-byte bound", max)
	}
	if previous, ok := f.floors[role]; ok {
		claim, err := downloadedRoleClaim(role, raw)
		if err != nil {
			return nil, err
		}
		// This check does not authenticate a new role. TUF still verifies all
		// signatures and bindings. It only refuses a payload that conflicts
		// with metadata this installation already authenticated, including a
		// same-version timestamp TUF would ordinarily discard unchanged.
		if claim.Version < previous.Version || claim.Version == previous.Version && claim.SHA256 != previous.SHA256 {
			return nil, errors.New("security feed downloaded metadata rollback or equivocation refused")
		}
	}
	return raw, nil
}

func downloadedRoleClaim(role string, raw []byte) (roleFloor, error) {
	switch role {
	case metadata.ROOT:
		value, err := metadata.Root().FromBytes(raw)
		if err != nil {
			return roleFloor{}, err
		}
		return signedRoleClaim(value.Signed)
	case metadata.TIMESTAMP:
		value, err := metadata.Timestamp().FromBytes(raw)
		if err != nil {
			return roleFloor{}, err
		}
		return signedRoleClaim(value.Signed)
	case metadata.SNAPSHOT:
		value, err := metadata.Snapshot().FromBytes(raw)
		if err != nil {
			return roleFloor{}, err
		}
		return signedRoleClaim(value.Signed)
	case metadata.TARGETS:
		value, err := metadata.Targets().FromBytes(raw)
		if err != nil {
			return roleFloor{}, err
		}
		return signedRoleClaim(value.Signed)
	default:
		return roleFloor{}, errors.New("unknown downloaded security feed role")
	}
}
