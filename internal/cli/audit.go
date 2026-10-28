package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strconv"
	"time"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/auditexport"
)

// runAudit addresses the existing proof-scoped readers. Instance scope is
// explicit, even when a context or environment selects a tenant by default.
func runAudit(ctx context.Context, ios IO, args []string) error {
	sub, rest, err := subverb("audit", args, "query", "export")
	if err != nil {
		return err
	}
	var instance bool
	var format string
	var after, ceiling int64
	var limit int
	query := url.Values{}
	st, flags, err := parseCommon("audit "+sub, ios, rest, func(fs *flag.FlagSet) {
		fs.BoolVar(&instance, "instance-scope", false, "read instance security events (human session only)")
		for _, key := range []string{"from", "to", "actor", "actor-name", "operation", "object-type", "object-id", "correlation-id"} {
			fs.Func(key, "audit filter (times use RFC3339)", func(value string) error {
				wireKey := key
				switch key {
				case "actor-name":
					wireKey = "actor_name"
				case "object-type":
					wireKey = "object_type"
				case "object-id":
					wireKey = "object_id"
				case "correlation-id":
					wireKey = "correlation_id"
				}
				if key == "from" || key == "to" {
					parsed, err := time.Parse(time.RFC3339Nano, value)
					if err != nil {
						return fmt.Errorf("--%s must be RFC3339: %w", key, err)
					}
					value = parsed.UTC().Format(time.RFC3339Nano)
				}
				query.Set(wireKey, value)
				return nil
			})
		}
		fs.Func("outcome", "repeat to match any selected outcome", func(value string) error {
			switch value {
			case "intent", "success", "denied", "failure", "unknown", "disconnected":
				if !slices.Contains(query["outcomes"], value) {
					query.Add("outcomes", value)
				}
				return nil
			default:
				return fmt.Errorf("invalid audit outcome %q", value)
			}
		})
		if sub == "query" {
			fs.StringVar(&format, "o", "table", "output format: table or json")
			fs.IntVar(&limit, "limit", 100, "rows per page (1 to 1000)")
			fs.Int64Var(&after, "after-seq", 0, "resume after this sequence")
			fs.Int64Var(&ceiling, "to-seq", 0, "pinned upper_seq from the first query page")
		}
	})
	if err != nil {
		return err
	}
	if err := flags.checkNoPositionals("audit " + sub); err != nil {
		return err
	}
	var output Format
	if sub == "query" {
		output, err = ParseFormat(format)
		if err != nil {
			return err
		}
		if limit < 1 || limit > 1000 || after < 0 || ceiling < 0 || (ceiling > 0 && after > ceiling) {
			return failf(ExitUsage, "audit query: limit must be 1 to 1000 and cursors must satisfy 0 <= after-seq <= to-seq")
		}
		query.Set("limit", strconv.Itoa(limit))
		query.Set("after_seq", strconv.FormatInt(after, 10))
		if ceiling != 0 {
			query.Set("to_seq", strconv.FormatInt(ceiling, 10))
		}
	}
	resolved, err := Resolve(st, ios.Env, flags.Flags, ios.Workdir)
	if err != nil {
		return err
	}
	path, err := auditScopePath(resolved, flags, instance)
	if err != nil {
		return err
	}
	client, artifact, _, err := authenticatedResolvedTarget(st, ios, flags, resolved)
	if err != nil {
		return err
	}
	if instance && artifact.Kind() != AuthKindHumanSession {
		return failf(ExitRefused, "instance audit requires a human session; project automation cannot collect the instance trail")
	}
	if sub == "export" {
		return client.ExportAudit(ctx, path+"/export?"+query.Encode(), ios.Stdout)
	}
	var page apigen.AuditPage
	if err := client.Do(ctx, http.MethodGet, path+"?"+query.Encode(), nil, &page); err != nil {
		return err
	}
	table := Table{Columns: []string{"SEQ", "OPERATION", "OUTCOME", "ACTOR", "SOURCE IP"}, JSON: page}
	for _, event := range page.Items {
		table.Rows = append(table.Rows, []string{strconv.FormatInt(event.Seq, 10), event.Type, event.Outcome, auditOptional(event.ActorId), auditOptional(event.SourceIp)})
	}
	if err := Render(ios.Stdout, output, table); err != nil {
		return err
	}
	if output == FormatTable {
		fmt.Fprintf(ios.Stderr, "next_after_seq=%d upper_seq=%d exhausted=%t\n", page.NextAfterSeq, page.UpperSeq, page.Exhausted)
	}
	return nil
}

// ExportAudit stages the stream privately so an incomplete response never
// becomes successful JSONL on stdout. #1036 owns negotiation and validation.
func (c *Client) ExportAudit(ctx context.Context, path string, dst io.Writer) (returnErr error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.Entry.Origin+path, nil)
	if err != nil {
		return err
	}
	if err := c.requireRevision(ctx, req); err != nil {
		return err
	}
	q := req.URL.Query()
	q.Set("receipt", "v1")
	req.URL.RawQuery = q.Encode()
	req.Header.Set("Accept", "application/x-ndjson")
	req.Header.Set("User-Agent", c.UserAgent)
	req.Header.Set("Authorization", "Bearer "+c.Bearer)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return failf(ExitUnavailable, "audit export: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if err != nil {
			return failf(ExitUnavailable, "reading audit refusal: %v", err)
		}
		return errorFromResponse(resp.StatusCode, resp.Header, body)
	}
	staged, err := os.CreateTemp("", "hikyo-audit-export-*")
	if err != nil {
		return err
	}
	defer func() {
		closeErr, removeErr := staged.Close(), os.Remove(staged.Name())
		if closeErr != nil || removeErr != nil {
			returnErr = errors.Join(returnErr, failf(ExitUnavailable, "cleaning audit staging: %v", errors.Join(closeErr, removeErr)))
		}
	}()
	if _, err := auditexport.Consume(resp, staged); err != nil {
		return failf(ExitUnavailable, "audit export incomplete; stdout was not published: %v", err)
	}
	if _, err := staged.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if _, err := io.Copy(dst, staged); err != nil {
		return failf(ExitUnavailable, "writing audit export: %v", err)
	}
	return nil
}

func auditOptional(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func auditScopePath(resolved Resolved, flags commonFlags, instance bool) (string, error) {
	if instance {
		if flags.Org != "" || flags.Project != "" || flags.Env != "" {
			return "", failf(ExitUsage, "--instance-scope and --org/--project/--env name different scopes; choose one")
		}
		return api.PathPrefix + "/instance/audit", nil
	}
	scope, err := NewTenantScope(resolved)
	if err != nil {
		return "", err
	}
	if scope.Kind() == TenantScopeInstance {
		return "", failf(ExitUsage, "audit needs --org or explicit --instance-scope")
	}
	path := api.PathPrefix + "/orgs/" + url.PathEscape(scope.Get(DimOrg))
	if project := scope.Get(DimProject); project != "" {
		path += "/projects/" + url.PathEscape(project)
		if env := scope.Get(DimEnv); env != "" {
			path += "/environments/" + url.PathEscape(env)
		}
	}
	return path + "/audit", nil
}
