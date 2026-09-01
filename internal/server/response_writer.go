package server

import "net/http"

// responseWriter is the one response-commit tracker shared by metrics and
// panic recovery. A bare Write or Flush commits 200, matching net/http.
type responseWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
	unmatched   bool
}

func newResponseWriter(w http.ResponseWriter) *responseWriter {
	return &responseWriter{ResponseWriter: w, status: http.StatusOK}
}

func (w *responseWriter) WriteHeader(code int) {
	if w.wroteHeader {
		return
	}
	// Informational responses do not commit the final status. net/http permits
	// any number of 1xx responses before one final 2xx-5xx response.
	if code >= 100 && code < 200 {
		w.ResponseWriter.WriteHeader(code)
		return
	}
	w.status = code
	w.wroteHeader = true
	w.ResponseWriter.WriteHeader(code)
}

func (w *responseWriter) Write(body []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(body)
}

func (w *responseWriter) Flush() {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// markRecoveredPanic changes only the observational status. When bytes were
// already committed, the wire response stays untouched while RED/error metrics
// still count the recovered fault as 5xx.
func (w *responseWriter) markRecoveredPanic() { w.status = http.StatusInternalServerError }

// markUnmatched keeps unsupported methods in the fail-closed `other` class
// even when chi retains the path pattern it matched before rejecting the verb.
func (w *responseWriter) markUnmatched() { w.unmatched = true }
