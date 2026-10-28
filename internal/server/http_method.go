package server

import "net/http"

// Methods outside the closed HTTP vocabulary can contain attacker prose or a
// bearer-shaped canary. Log the class, never that raw request field.
func loggedHTTPMethod(method string) string {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions, http.MethodConnect, http.MethodTrace:
		return method
	default:
		return "other"
	}
}
