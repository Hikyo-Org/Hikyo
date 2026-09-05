package server_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/internal/definitions"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/server"
	"github.com/Hikyo-Org/hikyo/internal/service"
)

type parsingDefinitions struct {
	stubDefinitions
	parsed chan int
}

func (s parsingDefinitions) Check(ctx context.Context, actor service.Actor, scope domain.Scope, raw []byte) (service.CheckResult, error) {
	s.parsed <- len(raw)
	if _, err := definitions.Parse(raw); err != nil {
		return service.CheckResult{}, err
	}
	return s.stubDefinitions.Check(ctx, actor, scope, raw)
}

func TestDefinitionBodyTransportPreservesTheDomainBound(t *testing.T) {
	for _, size := range []int{definitions.MaxBundleBytes, definitions.MaxBundleBytes + 1, server.MaxRequestBytes + 1} {
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			parsed := make(chan int, 1)
			srv := definitionsServer(t, parsingDefinitions{parsed: parsed})
			base := []byte(`{"format_version":1,"environments":[],"key_groups":[],"keys":[]}`)
			raw := append([]byte("{"), bytes.Repeat([]byte(" "), size-len(base))...)
			raw = append(raw, base[1:]...)
			path := api.PathPrefix + "/orgs/" + testOrgID + "/projects/" + testProjectID + "/definitions/check"
			req, err := http.NewRequest(http.MethodPost, srv.URL+path, bytes.NewReader(raw))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer hik_1_cli_x")
			resp, err := srv.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatal(err)
			}
			if size <= definitions.MaxBundleBytes {
				if resp.StatusCode != http.StatusOK {
					t.Fatalf("legal bundle: %d %s", resp.StatusCode, body)
				}
			} else if size <= server.MaxRequestBytes {
				if resp.StatusCode != http.StatusConflict || !strings.Contains(string(body), `"code":"limit_exceeded"`) {
					t.Fatalf("missing named domain refusal: %d %s", resp.StatusCode, body)
				}
			} else if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("global transport bound: %d %s", resp.StatusCode, body)
			}
			select {
			case got := <-parsed:
				if got != size || size > server.MaxRequestBytes {
					t.Fatalf("parser saw %d bytes for %d-byte request", got, size)
				}
			default:
				if size <= server.MaxRequestBytes {
					t.Fatal("transport prevented domain parsing")
				}
			}
		})
	}
}
