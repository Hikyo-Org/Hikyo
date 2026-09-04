// mcp-public-smoke verifies a deployed Hikyo MCP endpoint without printing
// credentials, tenant data, or tool results. Operators supply live and rotating
// credentials through environment variables. The checker first proves
// the rotating credential works, then waits for an operator to revoke it and
// observes the first tenant-safe denial.
package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/mcpserver"
	"github.com/Hikyo-Org/hikyo/internal/schema"
)

const maxResponseBytes = 1 << 20

const (
	defaultRevocationTimeout = 2 * time.Minute
	// One probe per two refill intervals stays below the credential's shared
	// 60/minute rate even when the operator uses the full revocation window.
	revocationPollInterval = 2 * time.Second
)

type options struct {
	endpoint          string
	tokenEnv          string
	rotatingTokenEnv  string
	orgID             string
	projectID         string
	revocationTimeout time.Duration
	status            io.Writer
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type publicDefinition struct {
	Name            *string               `json:"name"`
	Description     *string               `json:"description"`
	Classification  *string               `json:"classification"`
	Deprecated      *bool                 `json:"deprecated"`
	DeprecationNote string                `json:"deprecation_note,omitempty"`
	GroupID         string                `json:"group_id,omitempty"`
	Declaration     *schema.Declaration   `json:"declaration"`
	Presence        *schema.PresenceRules `json:"presence"`
}

func main() {
	var opts options
	flag.StringVar(&opts.endpoint, "url", "", "canonical public HTTPS MCP URL")
	flag.StringVar(&opts.tokenEnv, "token-env", "HIKYO_MCP_TOKEN", "environment variable holding the live credential")
	flag.StringVar(&opts.rotatingTokenEnv, "rotating-token-env", "HIKYO_MCP_ROTATING_TOKEN", "environment variable holding the still-live credential that will be revoked during this run")
	flag.StringVar(&opts.orgID, "org", "", "organization immutable ID")
	flag.StringVar(&opts.projectID, "project", "", "project immutable ID")
	flag.DurationVar(&opts.revocationTimeout, "revocation-timeout", defaultRevocationTimeout, "maximum time to observe revocation")
	flag.Parse()
	opts.status = os.Stderr

	client := &http.Client{
		Timeout: mcpserver.ToolExecutionTimeout + 5*time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	if err := run(opts, client); err != nil {
		fmt.Fprintln(os.Stderr, "MCP public smoke failed:", err)
		os.Exit(1)
	}
	fmt.Println("MCP public smoke passed: discovery, catalog, authorized read, invalid token, and revoked token")
}

func run(opts options, client *http.Client) error {
	if err := validateOptions(opts); err != nil {
		return err
	}
	live := os.Getenv(opts.tokenEnv)
	rotating := os.Getenv(opts.rotatingTokenEnv)
	if live == "" || rotating == "" {
		return fmt.Errorf("%s and %s must both be set", opts.tokenEnv, opts.rotatingTokenEnv)
	}
	if live == rotating {
		return errors.New("live and rotating credentials must differ")
	}
	client = noRedirectClient(client)

	discovery, sessionID, err := invoke(client, opts.endpoint, "server/discover", "", nil, "")
	if err != nil || discovery.Error != nil {
		return fmt.Errorf("unauthenticated discovery: %w", responseError(err, discovery.Error))
	}
	if err := requireStateless("discovery", sessionID); err != nil {
		return err
	}
	if err := validateDiscovery(discovery.Result); err != nil {
		return fmt.Errorf("unauthenticated discovery: %w", err)
	}
	catalog, sessionID, err := invoke(client, opts.endpoint, "tools/list", "", nil, "")
	if err != nil || catalog.Error != nil {
		return fmt.Errorf("unauthenticated closed tool catalog: %w", responseError(err, catalog.Error))
	}
	if err := requireStateless("tool catalog", sessionID); err != nil {
		return err
	}
	if err := validateCatalog(catalog.Result); err != nil {
		return fmt.Errorf("unauthenticated closed tool catalog: %w", err)
	}

	args := map[string]any{"org_id": opts.orgID, "project_id": opts.projectID, "page_size": 1}
	authorized, sessionID, err := invoke(client, opts.endpoint, "tools/call", mcpserver.ToolListDefinitions, args, live)
	if err != nil || authorized.Error != nil || !successfulToolResult(authorized.Result, opts.orgID, opts.projectID) {
		return fmt.Errorf("authorized safe read: %w", responseError(err, authorized.Error))
	}
	if err := requireStateless("authorized safe read", sessionID); err != nil {
		return err
	}

	previouslyLive, sessionID, err := invoke(client, opts.endpoint, "tools/call", mcpserver.ToolListDefinitions, args, rotating)
	if err != nil || previouslyLive.Error != nil || !successfulToolResult(previouslyLive.Result, opts.orgID, opts.projectID) {
		return fmt.Errorf("rotating credential was not live before revocation: %w", responseError(err, previouslyLive.Error))
	}
	if err := requireStateless("pre-revocation safe read", sessionID); err != nil {
		return err
	}
	if opts.status != nil {
		fmt.Fprintln(opts.status, "MCP rotating credential proved live; revoke it now")
	}

	invalidToken, err := randomInvalidToken()
	if err != nil {
		return err
	}
	invalid, sessionID, err := invoke(client, opts.endpoint, "tools/call", mcpserver.ToolListDefinitions, args, invalidToken)
	if err != nil {
		return fmt.Errorf("invalid-token probe: %w", err)
	}
	if err := requireStateless("invalid-token denial", sessionID); err != nil {
		return err
	}
	invalidMessage := safeToolError(invalid.Result)
	if invalidMessage != mcpserver.SafeOperationError {
		return errors.New("invalid credential did not receive the exact tenant-safe denial")
	}

	deadline := time.Now().Add(opts.revocationTimeout)
	for {
		rotated, sessionID, err := invoke(client, opts.endpoint, "tools/call", mcpserver.ToolListDefinitions, args, rotating)
		if err != nil {
			return fmt.Errorf("revocation probe: %w", err)
		}
		if err := requireStateless("revoked-token denial", sessionID); err != nil {
			return err
		}
		if safeToolError(rotated.Result) == invalidMessage {
			if !bytes.Equal(bytes.TrimSpace(invalid.Result), bytes.TrimSpace(rotated.Result)) {
				return errors.New("revoked credential denial differed from the invalid-token denial")
			}
			return nil
		}
		if rotated.Error != nil || !successfulToolResult(rotated.Result, opts.orgID, opts.projectID) {
			return errors.New("rotating credential returned an unexpected response while waiting for revocation")
		}
		if !time.Now().Before(deadline) {
			return errors.New("rotating credential remained authorized until the revocation deadline")
		}
		time.Sleep(revocationPollInterval)
	}
}

func noRedirectClient(client *http.Client) *http.Client {
	clone := *client
	clone.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &clone
}

func requireStateless(operation, sessionID string) error {
	if sessionID != "" {
		return fmt.Errorf("%s emitted an MCP session ID", operation)
	}
	return nil
}

func validateDiscovery(result json.RawMessage) error {
	var fields map[string]json.RawMessage
	if json.Unmarshal(result, &fields) != nil || len(fields) != 2 ||
		fields["supportedVersions"] == nil || fields["capabilities"] == nil {
		return errors.New("result did not match the exact discovery profile")
	}
	var discovery struct {
		SupportedVersions []string                   `json:"supportedVersions"`
		Capabilities      map[string]json.RawMessage `json:"capabilities"`
	}
	if json.Unmarshal(result, &discovery) != nil {
		return errors.New("result did not match the discovery profile")
	}
	if len(discovery.SupportedVersions) != 1 || discovery.SupportedVersions[0] != mcpserver.ProtocolVersion {
		return errors.New("server did not advertise only the pinned protocol version")
	}
	tools, ok := discovery.Capabilities["tools"]
	if !ok || len(discovery.Capabilities) != 1 {
		return errors.New("server capabilities were not the exact tools-only profile")
	}
	var toolCapabilities map[string]json.RawMessage
	if json.Unmarshal(tools, &toolCapabilities) != nil || len(toolCapabilities) != 0 {
		return errors.New("server advertised unapproved tool capabilities")
	}
	return nil
}

func validateCatalog(result json.RawMessage) error {
	var fields map[string]json.RawMessage
	if json.Unmarshal(result, &fields) != nil || len(fields) != 1 || fields["tools"] == nil {
		return errors.New("result did not match the exact tool catalog profile")
	}
	var catalog struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if json.Unmarshal(result, &catalog) != nil {
		return errors.New("result did not match the tool catalog profile")
	}
	got := make([]string, 0, len(catalog.Tools))
	for _, tool := range catalog.Tools {
		got = append(got, tool.Name)
	}
	want := mcpserver.ProductionToolNames()
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		return errors.New("server did not advertise the exact closed production tool catalog")
	}
	return nil
}

func validateOptions(opts options) error {
	parsed, err := url.Parse(opts.endpoint)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil ||
		parsed.Path != mcpserver.Path || parsed.RawQuery != "" || parsed.Fragment != "" ||
		opts.endpoint != "https://"+parsed.Host+mcpserver.Path {
		return errors.New("url must be the canonical public HTTPS endpoint ending in /mcp")
	}
	if opts.orgID == "" || opts.projectID == "" {
		return errors.New("org and project are required")
	}
	if opts.tokenEnv == "" || opts.rotatingTokenEnv == "" {
		return errors.New("credential environment variable names are required")
	}
	if opts.revocationTimeout <= 0 {
		return errors.New("revocation timeout must be positive")
	}
	return nil
}

func invoke(client *http.Client, endpoint, method, tool string, arguments map[string]any, token string) (rpcResponse, string, error) {
	meta := map[string]any{
		"io.modelcontextprotocol/protocolVersion":    mcpserver.ProtocolVersion,
		"io.modelcontextprotocol/clientCapabilities": map[string]any{},
		"io.modelcontextprotocol/clientInfo": map[string]string{
			"name": "hikyo-public-smoke", "version": "1",
		},
	}
	params := map[string]any{"_meta": meta}
	if method == "tools/call" {
		params["name"] = tool
		params["arguments"] = arguments
	}
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": method, "params": params,
	})
	if err != nil {
		return rpcResponse{}, "", err
	}
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return rpcResponse{}, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Mcp-Protocol-Version", mcpserver.ProtocolVersion)
	req.Header.Set("Mcp-Method", method)
	if tool != "" {
		req.Header.Set("Mcp-Name", tool)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := client.Do(req)
	if err != nil {
		return rpcResponse{}, "", err
	}
	defer response.Body.Close()
	encoded, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return rpcResponse{}, "", err
	}
	if len(encoded) > maxResponseBytes {
		return rpcResponse{}, "", errors.New("response exceeded smoke-test bound")
	}
	if response.StatusCode != http.StatusOK {
		return rpcResponse{}, "", fmt.Errorf("HTTP status %d", response.StatusCode)
	}
	decoded, err := decodeRPCResponse(encoded)
	if err != nil {
		return rpcResponse{}, "", err
	}
	return decoded, response.Header.Get("Mcp-Session-Id"), nil
}

func decodeRPCResponse(encoded []byte) (rpcResponse, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		return rpcResponse{}, errors.New("response was not JSON-RPC")
	}
	if len(fields) != 3 || fields["jsonrpc"] == nil || fields["id"] == nil ||
		(fields["result"] == nil) == (fields["error"] == nil) {
		return rpcResponse{}, errors.New("response was not the exact JSON-RPC envelope")
	}
	var decoded rpcResponse
	if err := json.Unmarshal(encoded, &decoded); err != nil || decoded.JSONRPC != "2.0" || string(decoded.ID) != "1" {
		return rpcResponse{}, errors.New("response JSON-RPC version or request id did not match")
	}
	if decoded.Error != nil {
		var errorFields map[string]json.RawMessage
		if json.Unmarshal(fields["error"], &errorFields) != nil || len(errorFields) != 2 ||
			errorFields["code"] == nil || errorFields["message"] == nil {
			return rpcResponse{}, errors.New("response carried a non-canonical JSON-RPC error")
		}
	}
	return decoded, nil
}

func safeToolError(result json.RawMessage) string {
	var fields map[string]json.RawMessage
	if json.Unmarshal(result, &fields) != nil || len(fields) != 2 || fields["isError"] == nil || fields["content"] == nil {
		return ""
	}
	var toolResult struct {
		IsError bool `json:"isError"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if json.Unmarshal(result, &toolResult) != nil || !toolResult.IsError || len(toolResult.Content) != 1 {
		return ""
	}
	var contentFields map[string]json.RawMessage
	var contentItems []json.RawMessage
	if json.Unmarshal(fields["content"], &contentItems) != nil || len(contentItems) != 1 ||
		json.Unmarshal(contentItems[0], &contentFields) != nil || len(contentFields) != 2 ||
		contentFields["type"] == nil || contentFields["text"] == nil || toolResult.Content[0].Type != "text" {
		return ""
	}
	return toolResult.Content[0].Text
}

func successfulToolResult(result json.RawMessage, orgID, projectID string) bool {
	var fields map[string]json.RawMessage
	if json.Unmarshal(result, &fields) != nil || len(fields) != 2 || fields["content"] == nil || fields["structuredContent"] == nil {
		return false
	}
	var content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(fields["content"], &content) != nil || len(content) != 1 ||
		content[0].Type != "text" || content[0].Text != "Hikyo returned structured data." {
		return false
	}
	var contentItems []json.RawMessage
	var contentFields map[string]json.RawMessage
	if json.Unmarshal(fields["content"], &contentItems) != nil || len(contentItems) != 1 ||
		json.Unmarshal(contentItems[0], &contentFields) != nil || len(contentFields) != 2 ||
		contentFields["type"] == nil || contentFields["text"] == nil {
		return false
	}
	var structuredFields map[string]json.RawMessage
	if json.Unmarshal(fields["structuredContent"], &structuredFields) != nil ||
		(len(structuredFields) != 4 && len(structuredFields) != 5) ||
		structuredFields["org_id"] == nil || structuredFields["project_id"] == nil ||
		structuredFields["schema_revision"] == nil || structuredFields["definitions"] == nil {
		return false
	}
	for key := range structuredFields {
		if key != "org_id" && key != "project_id" && key != "schema_revision" && key != "definitions" && key != "next_cursor" {
			return false
		}
	}
	var structured struct {
		OrgID          string             `json:"org_id"`
		ProjectID      string             `json:"project_id"`
		SchemaRevision int64              `json:"schema_revision"`
		Definitions    []publicDefinition `json:"definitions"`
		NextCursor     string             `json:"next_cursor,omitempty"`
	}
	if strictJSON(fields["structuredContent"], &structured) != nil ||
		structured.OrgID != orgID || structured.ProjectID != projectID || structured.Definitions == nil {
		return false
	}
	for _, definition := range structured.Definitions {
		if definition.Name == nil || definition.Description == nil || definition.Classification == nil ||
			definition.Deprecated == nil || definition.Declaration == nil || definition.Presence == nil {
			return false
		}
		classification := schema.Classification(*definition.Classification)
		if schema.CheckKeyName(*definition.Name) != nil || !classification.Valid() ||
			schema.CheckDescription("description", *definition.Description) != nil {
			return false
		}
		if _, err := schema.CompileClassified(classification, *definition.Declaration); err != nil {
			return false
		}
		if err := schema.CheckPresence(definition.Presence.Required, definition.Presence.Forbidden); err != nil {
			return false
		}
	}
	return true
}

func strictJSON(raw []byte, into any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(into); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("trailing JSON content")
	}
	return nil
}

func responseError(err error, rpcErr *struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}) error {
	if err != nil {
		return err
	}
	if rpcErr != nil {
		return fmt.Errorf("JSON-RPC code %d", rpcErr.Code)
	}
	return errors.New("missing expected result")
}

func randomInvalidToken() (string, error) {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", errors.New("generate invalid-token probe")
	}
	return "invalid_" + strings.ToLower(hex.EncodeToString(raw)), nil
}
