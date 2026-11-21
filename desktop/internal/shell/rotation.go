package shell

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"

	"github.com/Hikyo-Org/hikyo/internal/crypto"
)

// tokenView is what the shell reads from a JSON response: a session token and
// the identifiers that bind it to the connection. Only the top level and the
// `login` member are read, because a nested key could be a secret's name
// (ADR D4).
type tokenView struct {
	Token       string
	SessionID   string
	PrincipalID string
	// Body is the response with the token removed.
	Body []byte
}

var errNotObject = errors.New("not a JSON object")

// splitToken parses a response body. A body that is not a JSON object cannot
// carry a token and is returned as is with errNotObject. Invalid JSON and
// duplicate names are errors, and callers fail closed on them.
func splitToken(raw []byte) (tokenView, error) {
	trimmed := raw
	for len(trimmed) > 0 && (trimmed[0] == ' ' || trimmed[0] == '\n' || trimmed[0] == '\t' || trimmed[0] == '\r') {
		trimmed = trimmed[1:]
	}
	if len(trimmed) == 0 {
		return tokenView{Body: raw}, errNotObject
	}
	if trimmed[0] != '{' {
		// A valid array or scalar can't carry a token; a truncated one is not
		// known to be token-free.
		if !jsontext.Value(trimmed).IsValid() {
			return tokenView{}, errors.New("the body is not valid JSON")
		}
		return tokenView{Body: raw}, errNotObject
	}
	top, err := object(raw)
	if err != nil {
		return tokenView{}, err
	}
	view := tokenView{Body: raw}
	var loginMembers map[string]jsontext.Value
	if rawLogin, ok := top["login"]; ok && !isNull(rawLogin) {
		if loginMembers, err = object(rawLogin); err != nil {
			// A `login` member that is not an object carries no token.
			loginMembers = nil
		}
	}
	changed := false
	if _, ok := top["session_token"]; ok {
		tok, ok := stringMember(top, "session_token")
		if !ok || tok == "" {
			return tokenView{}, errors.New("session_token is not a non-empty string")
		}
		view.Token = tok
		delete(top, "session_token")
		changed = true
	}
	if loginMembers != nil {
		if _, ok := loginMembers["session_token"]; ok {
			tok, ok := stringMember(loginMembers, "session_token")
			if !ok || tok == "" || view.Token != "" {
				return tokenView{}, errors.New("session_token is malformed or repeated")
			}
			view.Token = tok
			delete(loginMembers, "session_token")
			nested, err := json.Marshal(loginMembers)
			if err != nil {
				return tokenView{}, err
			}
			top["login"] = nested
			changed = true
		}
	}
	view.SessionID = firstNonEmpty(idAt(top, "session_id"), nestedID(top, "session"), idAt(loginMembers, "session_id"), nestedID(loginMembers, "session"))
	view.PrincipalID = firstNonEmpty(nestedID(top, "principal"), nestedID(loginMembers, "principal"))
	if changed {
		if view.Body, err = json.Marshal(top); err != nil {
			return tokenView{}, err
		}
	}
	return view, nil
}

func idAt(m map[string]jsontext.Value, key string) string {
	s, _ := stringMember(m, key)
	return s
}

func nestedID(m map[string]jsontext.Value, key string) string {
	raw, ok := m[key]
	if !ok {
		return ""
	}
	inner, err := object(raw)
	if err != nil {
		return ""
	}
	return idAt(inner, "id")
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// parsesAsCLIToken applies the CLI token grammar (hik_<v>_cli_...).
func parsesAsCLIToken(token string) bool {
	return crypto.ParseArtifact(token, crypto.ArtifactCLISession) == nil
}

// checkRotation applies the ADR D4 checks to a token a listed route returned.
// A rotate response must keep the stored session id; a reissue must keep the
// stored principal and names the new session id.
func checkRotation(kind Rotation, view tokenView, stored Secret, principalID string) (newSessionID string, err error) {
	if !parsesAsCLIToken(view.Token) {
		return "", errors.New("the returned token is not a CLI session token")
	}
	switch kind {
	case Rotate:
		if view.SessionID == "" || view.SessionID != stored.SessionID {
			return "", errors.New("the rotated session is not this connection's session")
		}
		return stored.SessionID, nil
	case Reissue:
		if view.PrincipalID == "" || principalID == "" || view.PrincipalID != principalID {
			return "", errors.New("the reissued session belongs to another principal")
		}
		if view.SessionID == "" {
			return "", errors.New("the reissued session has no id")
		}
		return view.SessionID, nil
	default:
		return "", fmt.Errorf("route does not rotate sessions")
	}
}
