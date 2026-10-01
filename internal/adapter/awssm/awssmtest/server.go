// Package awssmtest is an isolated, in-process AWS Secrets Manager and STS
// emulator for adapter tests. It speaks the real wire protocols (awsJson1.1
// for Secrets Manager, the STS query protocol), refuses unsigned requests,
// and records every operation so tests can prove no value-read API was
// called. Only the test oracle Value reads a stored value.
package awssmtest

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

type version struct {
	value  string
	stages []string
}

type secret struct {
	name     string
	arn      string
	kms      string
	tags     map[string]string
	versions map[string]*version
	deleted  bool
}

// Server is one emulated account in one region.
type Server struct {
	*httptest.Server
	Account string
	Region  string

	mu         sync.Mutex
	secrets    map[string]*secret
	operations []string
	// Fail maps an operation name to an AWS error type returned once.
	fail map[string]failure
}

type failure struct {
	status     int
	code       string
	retryAfter string
}

// New starts a TLS emulator. Callers trust Certificate() explicitly.
func New(account, region string) *Server {
	s := &Server{Account: account, Region: region, secrets: map[string]*secret{}, fail: map[string]failure{}}
	s.Server = httptest.NewTLSServer(http.HandlerFunc(s.serve))
	return s
}

// FailNext makes the next call of operation fail with an AWS error type.
func (s *Server) FailNext(operation string, status int, code, retryAfter string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fail[operation] = failure{status: status, code: code, retryAfter: retryAfter}
}

// Operations returns every operation name received, in order.
func (s *Server) Operations() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.operations)
}

// Value is the external test oracle: the AWSCURRENT SecretString.
func (s *Server) Value(name string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sec, ok := s.secrets[name]
	if !ok {
		return "", false
	}
	for _, v := range sec.versions {
		if slices.Contains(v.stages, "AWSCURRENT") {
			return v.value, true
		}
	}
	return "", false
}

// Versions reports how many versions a secret holds.
func (s *Server) Versions(name string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sec, ok := s.secrets[name]; ok {
		return len(sec.versions)
	}
	return 0
}

// Deleted reports whether a secret is scheduled for deletion.
func (s *Server) Deleted(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	sec, ok := s.secrets[name]
	return ok && sec.deleted
}

// Tag returns one tag value.
func (s *Server) Tag(name, key string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sec, ok := s.secrets[name]; ok {
		return sec.tags[key]
	}
	return ""
}

// Seed creates a secret outside Hikyo, as a third party would.
func (s *Server) Seed(name, value string, tags map[string]string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sec := s.newSecret(name, "", tags)
	sec.versions["seed"] = &version{value: value, stages: []string{"AWSCURRENT"}}
}

// ExternalPut writes a new version the way an operator console edit does:
// it moves only AWSCURRENT.
func (s *Server) ExternalPut(name, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sec := s.secrets[name]
	id := fmt.Sprintf("external-%d", len(sec.versions))
	moveStages(sec, id, "AWSCURRENT")
	sec.versions[id] = &version{value: value, stages: []string{"AWSCURRENT"}}
}

func (s *Server) newSecret(name, kms string, tags map[string]string) *secret {
	sec := &secret{name: name, kms: kms, tags: map[string]string{}, versions: map[string]*version{},
		arn: "arn:aws:secretsmanager:" + s.Region + ":" + s.Account + ":secret:" + name + "-Ab12Cd"}
	for k, v := range tags {
		sec.tags[k] = v
	}
	s.secrets[name] = sec
	return sec
}

func moveStages(sec *secret, target string, stages ...string) {
	for _, v := range sec.versions {
		v.stages = slices.DeleteFunc(v.stages, func(stage string) bool { return slices.Contains(stages, stage) })
	}
	if v, ok := sec.versions[target]; ok {
		v.stages = append(v.stages, stages...)
	}
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	auth := r.Header.Get("Authorization")
	if r.Method != http.MethodPost || !strings.HasPrefix(auth, "AWS4-HMAC-SHA256 Credential=") || r.Header.Get("X-Amz-Date") == "" {
		writeError(w, http.StatusForbidden, "MissingAuthenticationTokenException", "")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "SerializationException", "")
		return
	}
	target := r.Header.Get("X-Amz-Target")
	if target == "" {
		s.serveSTS(w, r, body, auth)
		return
	}
	if !strings.Contains(auth, "/"+s.Region+"/secretsmanager/aws4_request") {
		writeError(w, http.StatusBadRequest, "InvalidSignatureException", "")
		return
	}
	operation := strings.TrimPrefix(target, "secretsmanager.")
	s.mu.Lock()
	defer s.mu.Unlock()
	s.operations = append(s.operations, operation)
	if f, ok := s.fail[operation]; ok {
		delete(s.fail, operation)
		writeError(w, f.status, f.code, f.retryAfter)
		return
	}
	var in map[string]any
	if err := json.Unmarshal(body, &in); err != nil {
		writeError(w, http.StatusBadRequest, "SerializationException", "")
		return
	}
	id, _ := in["SecretId"].(string)
	switch operation {
	case "CreateSecret":
		name, _ := in["Name"].(string)
		if _, ok := s.secrets[name]; ok {
			writeError(w, http.StatusBadRequest, "ResourceExistsException", "")
			return
		}
		kms, _ := in["KmsKeyId"].(string)
		sec := s.newSecret(name, kms, parseTags(in["Tags"]))
		if value, ok := in["SecretString"].(string); ok {
			token, _ := in["ClientRequestToken"].(string)
			sec.versions[token] = &version{value: value, stages: []string{"AWSCURRENT"}}
		}
		writeJSON(w, map[string]string{"ARN": sec.arn, "Name": name})
	case "DescribeSecret":
		sec, ok := s.secrets[id]
		if !ok {
			writeError(w, http.StatusBadRequest, "ResourceNotFoundException", "")
			return
		}
		stages := map[string][]string{}
		for vid, v := range sec.versions {
			if len(v.stages) != 0 {
				stages[vid] = slices.Clone(v.stages)
			}
		}
		out := map[string]any{"ARN": sec.arn, "Name": sec.name, "Tags": tagList(sec.tags), "VersionIdsToStages": stages}
		if sec.kms != "" {
			out["KmsKeyId"] = sec.kms
		}
		if sec.deleted {
			out["DeletedDate"] = float64(time.Now().Unix())
		}
		writeJSON(w, out)
	case "ListSecrets":
		prefix := ""
		if filters, ok := in["Filters"].([]any); ok && len(filters) == 1 {
			if f, ok := filters[0].(map[string]any); ok {
				if values, ok := f["Values"].([]any); ok && len(values) == 1 {
					prefix, _ = values[0].(string)
				}
			}
		}
		names := make([]string, 0, len(s.secrets))
		for name := range s.secrets {
			// AWS's name filter is a case-insensitive prefix match.
			if strings.HasPrefix(strings.ToLower(name), strings.ToLower(prefix)) {
				names = append(names, name)
			}
		}
		sort.Strings(names)
		list := make([]map[string]any, 0, len(names))
		for _, name := range names {
			list = append(list, map[string]any{"Name": name, "ARN": s.secrets[name].arn})
		}
		writeJSON(w, map[string]any{"SecretList": list})
	case "PutSecretValue":
		sec, ok := s.secrets[id]
		if !ok {
			writeError(w, http.StatusBadRequest, "ResourceNotFoundException", "")
			return
		}
		if sec.deleted {
			writeError(w, http.StatusBadRequest, "InvalidRequestException", "")
			return
		}
		token, _ := in["ClientRequestToken"].(string)
		value, _ := in["SecretString"].(string)
		if prior, ok := sec.versions[token]; ok {
			if prior.value != value {
				writeError(w, http.StatusBadRequest, "ResourceExistsException", "")
				return
			}
			writeJSON(w, map[string]string{"ARN": sec.arn, "VersionId": token})
			return
		}
		stages := []string{"AWSCURRENT"}
		if raw, ok := in["VersionStages"].([]any); ok {
			stages = stages[:0]
			for _, stage := range raw {
				if text, ok := stage.(string); ok {
					stages = append(stages, text)
				}
			}
		}
		// AWS makes its first value current even with explicit custom stages.
		if len(sec.versions) == 0 && !slices.Contains(stages, "AWSCURRENT") {
			stages = append(stages, "AWSCURRENT")
		}
		sec.versions[token] = &version{value: value}
		moveStages(sec, token, stages...)
		writeJSON(w, map[string]string{"ARN": sec.arn, "VersionId": token})
	case "UpdateSecretVersionStage":
		sec, ok := s.secrets[id]
		if !ok {
			writeError(w, http.StatusBadRequest, "ResourceNotFoundException", "")
			return
		}
		if sec.deleted {
			writeError(w, http.StatusBadRequest, "InvalidRequestException", "")
			return
		}
		stage, _ := in["VersionStage"].(string)
		moveTo, _ := in["MoveToVersionId"].(string)
		removeFrom, _ := in["RemoveFromVersionId"].(string)
		if _, exists := sec.versions[moveTo]; !exists || stage == "" {
			writeError(w, http.StatusBadRequest, "InvalidParameterException", "")
			return
		}
		owner := ""
		for versionID, v := range sec.versions {
			if slices.Contains(v.stages, stage) {
				owner = versionID
			}
		}
		if (owner != "" && owner != moveTo && removeFrom != owner) || (removeFrom != "" && removeFrom != owner) {
			writeError(w, http.StatusBadRequest, "InvalidParameterException", "")
			return
		}
		moveStages(sec, moveTo, stage)
		if stage == "AWSCURRENT" && owner != "" && owner != moveTo {
			moveStages(sec, owner, "AWSPREVIOUS")
		}
		writeJSON(w, map[string]string{"ARN": sec.arn, "Name": sec.name})
	case "TagResource":
		sec, ok := s.secrets[id]
		if !ok {
			writeError(w, http.StatusBadRequest, "ResourceNotFoundException", "")
			return
		}
		for k, v := range parseTags(in["Tags"]) {
			sec.tags[k] = v
		}
		writeJSON(w, map[string]string{})
	case "RestoreSecret":
		sec, ok := s.secrets[id]
		if !ok {
			writeError(w, http.StatusBadRequest, "ResourceNotFoundException", "")
			return
		}
		sec.deleted = false
		writeJSON(w, map[string]string{"ARN": sec.arn})
	case "DeleteSecret":
		sec, ok := s.secrets[id]
		if !ok {
			writeError(w, http.StatusBadRequest, "ResourceNotFoundException", "")
			return
		}
		if force, _ := in["ForceDeleteWithoutRecovery"].(bool); force {
			delete(s.secrets, id)
		} else {
			sec.deleted = true
		}
		writeJSON(w, map[string]string{"ARN": sec.arn})
	default:
		// GetSecretValue, BatchGetSecretValue, and every other operation
		// are recorded above and refused here.
		writeError(w, http.StatusBadRequest, "UnsupportedOperationException", "")
	}
}

func (s *Server) serveSTS(w http.ResponseWriter, r *http.Request, body []byte, auth string) {
	if !strings.Contains(auth, "/sts/aws4_request") {
		writeError(w, http.StatusBadRequest, "InvalidSignatureException", "")
		return
	}
	s.mu.Lock()
	s.operations = append(s.operations, "sts:GetCallerIdentity")
	s.mu.Unlock()
	if !strings.Contains(string(body), "Action=GetCallerIdentity") {
		http.Error(w, "unsupported", http.StatusBadRequest)
		return
	}
	type result struct {
		Arn     string `xml:"Arn"`
		UserID  string `xml:"UserId"`
		Account string `xml:"Account"`
	}
	type response struct {
		XMLName xml.Name `xml:"GetCallerIdentityResponse"`
		Xmlns   string   `xml:"xmlns,attr"`
		Result  result   `xml:"GetCallerIdentityResult"`
		Request struct {
			RequestID string `xml:"RequestId"`
		} `xml:"ResponseMetadata"`
	}
	out := response{Xmlns: "https://sts.amazonaws.com/doc/2011-06-15/", Result: result{Arn: "arn:aws:iam::" + s.Account + ":user/hikyo", UserID: "AIDAHIKYO", Account: s.Account}}
	out.Request.RequestID = "awssmtest"
	w.Header().Set("Content-Type", "text/xml")
	_ = xml.NewEncoder(w).Encode(out)
}

func parseTags(raw any) map[string]string {
	out := map[string]string{}
	list, _ := raw.([]any)
	for _, item := range list {
		if tag, ok := item.(map[string]any); ok {
			key, _ := tag["Key"].(string)
			value, _ := tag["Value"].(string)
			out[key] = value
		}
	}
	return out
}

func tagList(tags map[string]string) []map[string]string {
	out := make([]map[string]string, 0, len(tags))
	for k, v := range tags {
		out = append(out, map[string]string{"Key": k, "Value": v})
	}
	return out
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/x-amz-json-1.1")
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, retryAfter string) {
	w.Header().Set("Content-Type", "application/x-amz-json-1.1")
	w.Header().Set("X-Amzn-ErrorType", code)
	if retryAfter != "" {
		w.Header().Set("Retry-After", retryAfter)
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"__type": code, "message": "awssmtest " + code})
}
