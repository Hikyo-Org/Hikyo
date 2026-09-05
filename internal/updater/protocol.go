package updater

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"time"
)

const protocolBodyLimit = 1 << 20

type Capability struct {
	Backend Backend `json:"backend"`
}

// Control is the server-side seam. Implementations speak only to the local
// updater helper; remote Hikyo traffic always terminates at the remote's own
// authenticated API before reaching this interface.
type Control interface {
	Capability(context.Context) (Capability, error)
	Submit(context.Context, Request) (Job, error)
	Job(context.Context, string) (Job, error)
	AcknowledgeOutcome(context.Context, string) error
}

type ControlServer struct {
	Executor Executor
	Journal  *Journal
	Log      *slog.Logger
	Context  context.Context
}

func (s *ControlServer) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/capability", s.capability)
	mux.HandleFunc("POST /v1/jobs", s.submit)
	mux.HandleFunc("GET /v1/jobs/{job}", s.job)
	mux.HandleFunc("GET /v1/outcomes", s.outcomes)
	mux.HandleFunc("POST /v1/jobs/{job}/outcome-reported", s.acknowledge)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		mux.ServeHTTP(w, r)
	})
}

func (s *ControlServer) capability(w http.ResponseWriter, _ *http.Request) {
	writeProtocolError(w, http.StatusServiceUnavailable, "remote-apply-disabled")
}

func (s *ControlServer) submit(w http.ResponseWriter, _ *http.Request) {
	writeProtocolError(w, http.StatusServiceUnavailable, "remote-apply-disabled")
}

func (s *ControlServer) outcomes(w http.ResponseWriter, _ *http.Request) {
	if s.Journal == nil {
		writeProtocolError(w, http.StatusServiceUnavailable, "journal-unavailable")
		return
	}
	jobs, err := s.Journal.PendingOutcomes()
	if err != nil {
		writeProtocolError(w, http.StatusInternalServerError, "journal-read-failed")
		return
	}
	writeJSON(w, http.StatusOK, jobs)
}

func (s *ControlServer) job(w http.ResponseWriter, r *http.Request) {
	if s.Journal == nil {
		writeProtocolError(w, http.StatusServiceUnavailable, "journal-unavailable")
		return
	}
	job, err := s.Journal.Get(r.PathValue("job"))
	if errors.Is(err, ErrJobNotFound) {
		writeProtocolError(w, http.StatusNotFound, "job-not-found")
		return
	}
	if err != nil {
		writeProtocolError(w, http.StatusInternalServerError, "journal-read-failed")
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (s *ControlServer) acknowledge(w http.ResponseWriter, r *http.Request) {
	if s.Journal == nil {
		writeProtocolError(w, http.StatusServiceUnavailable, "journal-unavailable")
		return
	}
	job, err := s.Journal.Get(r.PathValue("job"))
	if errors.Is(err, ErrJobNotFound) {
		writeProtocolError(w, http.StatusNotFound, "job-not-found")
		return
	}
	if err != nil || !job.State.Terminal() {
		writeProtocolError(w, http.StatusConflict, "job-not-terminal")
		return
	}
	job.OutcomeReported = true
	if err := s.Journal.Put(job); err != nil {
		writeProtocolError(w, http.StatusInternalServerError, "journal-write-failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type protocolError struct {
	Code string `json:"code"`
}

func writeProtocolError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, protocolError{Code: code})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func decodeJSON(r io.Reader, value any) error {
	decoder := json.NewDecoder(io.LimitReader(r, protocolBodyLimit))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("multiple JSON values")
	}
	return nil
}

type Client struct {
	http *http.Client
}

func NewClient(socket string) *Client {
	dialer := &net.Dialer{Timeout: 2 * time.Second}
	transport := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, "unix", socket)
		},
		DisableKeepAlives: true,
	}
	return &Client{http: &http.Client{
		Transport: transport,
		Timeout:   5 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("updater: helper redirect refused")
		},
	}}
}

// Capability and Submit refuse locally even when the socket belongs to an old
// helper. Historical reads and outcome acknowledgement remain available.
func (c *Client) Capability(context.Context) (Capability, error) {
	return Capability{}, ErrRemoteApplyDisabled
}

func (c *Client) Submit(context.Context, Request) (Job, error) {
	return Job{}, ErrRemoteApplyDisabled
}

func (c *Client) Job(ctx context.Context, id string) (Job, error) {
	var job Job
	err := c.do(ctx, http.MethodGet, "/v1/jobs/"+url.PathEscape(id), nil, http.StatusOK, &job)
	return job, err
}

func (c *Client) AcknowledgeOutcome(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodPost, "/v1/jobs/"+url.PathEscape(id)+"/outcome-reported", struct{}{}, http.StatusNoContent, nil)
}

func (c *Client) PendingOutcomes(ctx context.Context) ([]Job, error) {
	var jobs []Job
	err := c.do(ctx, http.MethodGet, "/v1/outcomes", nil, http.StatusOK, &jobs)
	return jobs, err
}

func (c *Client) do(ctx context.Context, method, path string, body any, want int, out any) error {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(b)
	}
	request, err := http.NewRequestWithContext(ctx, method, "http://unix"+path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := c.http.Do(request)
	if err != nil {
		return fmt.Errorf("updater: helper unavailable: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != want {
		var refusal protocolError
		_ = decodeJSON(response.Body, &refusal)
		if refusal.Code == "remote-apply-disabled" {
			return ErrRemoteApplyDisabled
		}
		if refusal.Code == "update-active" {
			return ErrUpdateActive
		}
		if refusal.Code == "stable-only" {
			return ErrStableOnly
		}
		if refusal.Code == "release-authority" {
			return ErrReleaseAuthority
		}
		if refusal.Code == "job-not-found" {
			return ErrJobNotFound
		}
		return fmt.Errorf("updater: helper returned %d (%s)", response.StatusCode, refusal.Code)
	}
	if out == nil {
		_, err = io.Copy(io.Discard, io.LimitReader(response.Body, protocolBodyLimit))
		return err
	}
	if err := decodeJSON(response.Body, out); err != nil {
		return fmt.Errorf("updater: invalid helper response: %w", err)
	}
	return nil
}
