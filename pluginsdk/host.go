package plugin

import "fmt"

// Request is an HTTP request for Fetch.
type Request struct {
	Method  string            `json:"method,omitempty"` // GET when empty
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    []byte            `json:"body,omitempty"`
}

// Response is what Fetch got back.
type Response struct {
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    []byte            `json:"body,omitempty"`
}

type wireResponse struct {
	Response
	Error string `json:"error,omitempty"`
}

// Fetch makes an HTTP request through Matinee. Only https URLs on hosts the
// plugin's manifest lists under permissions.network are allowed. Bodies are
// limited to 32 MB.
func Fetch(req Request) (*Response, error) { return fetch(req) }

// Get fetches a URL and returns its body, or an error for a status other
// than 200.
func Get(url string) ([]byte, error) {
	res, err := Fetch(Request{URL: url})
	if err != nil {
		return nil, err
	}
	if res.Status != 200 {
		return nil, fmt.Errorf("%s: status %d", url, res.Status)
	}
	return res.Body, nil
}

// Debug, Info, Warn and Error write to the server's log, marked with the
// plugin's id.
func Debug(format string, args ...any) { logAt(0, fmt.Sprintf(format, args...)) }
func Info(format string, args ...any)  { logAt(1, fmt.Sprintf(format, args...)) }
func Warn(format string, args ...any)  { logAt(2, fmt.Sprintf(format, args...)) }
func Error(format string, args ...any) { logAt(3, fmt.Sprintf(format, args...)) }
