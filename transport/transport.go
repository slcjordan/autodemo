package transport

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httputil"
	"net/url"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/slcjordan/autodemo"
)

type HistoryListener interface {
	Notify(autodemo.History)
}

type Curl struct {
	mu    sync.RWMutex //guards jars, count
	jars  []http.CookieJar
	count atomic.Int32

	Transport http.RoundTripper
	Listener  HistoryListener
	Insecure  bool
}

func (c *Curl) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.jars = nil
	c.count.Store(0)
}

func (c *Curl) encodeCookies(cookies []*http.Cookie) string {
	var cookieStrings []string

	for _, c := range cookies {
		cookieStrings = append(cookieStrings, c.String())
	}
	return strings.Join(sort.StringSlice(cookieStrings), "&")
}

func (c *Curl) findMatchingJar(cookies []*http.Cookie, u *url.URL) (int, bool) {
	expected := c.encodeCookies(cookies)
	c.mu.RLock()
	defer c.mu.RUnlock()

	for i, j := range c.jars {
		if expected == c.encodeCookies(j.Cookies(u)) {
			return i, true
		}
	}
	return 0, false
}

func maybePrettify(uglyJSON string) string {
	var prettyJSON bytes.Buffer
	err := json.Indent(&prettyJSON, []byte(uglyJSON), "", "  ")
	if err != nil {
		return uglyJSON
	}
	return prettyJSON.String()
}

func (c *Curl) CurlFromRequest(req *http.Request) autodemo.History {
	var h autodemo.History
	h.Args = append(h.Args, "curl")
	if c.Insecure {
		h.Args = append(h.Args, "--insecure")
	}
	h.Args = append(h.Args, "-X", req.Method)

	for key, values := range req.Header {
		switch key {
		case "X-Forwarded-For", "Cookie", "User-Agent", "Accept-Encoding", "Content-Length":
			continue
		default:
		}
		for _, val := range values {
			h.Args = append(h.Args, "-H", fmt.Sprintf("\"%s: %s\"", key, val))
		}
	}

	if req.Body != nil && (req.Method == "POST" || req.Method == "PUT" || req.Method == "PATCH") {
		body, err := httputil.DumpRequest(req, true)
		if err == nil {
			bodyStr := string(body)
			parts := strings.SplitN(bodyStr, "\r\n\r\n", 2)
			if len(parts) > 1 {
				if req.Header.Get("Content-Type") == "application/json" {
					h.Args = append(h.Args, "--data", fmt.Sprintf("'%s'", maybePrettify(parts[1])))
				} else {
					h.Args = append(h.Args, "--data", fmt.Sprintf("'%s'", parts[1]))
				}
			}
		}
	}
	h.Args = append(h.Args, fmt.Sprintf("%q", req.URL.String()))

	h.Index = int(c.count.Add(1) - 1)
	return h
}

func (c *Curl) updateJar(idx int, u *url.URL, cookies []*http.Cookie) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.jars[idx].SetCookies(u, cookies)
}

func (c *Curl) addJar() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	idx := len(c.jars)
	jar, _ := cookiejar.New(nil)
	c.jars = append(c.jars, jar)
	return idx
}

func (c *Curl) curlResponseFormat(resp *http.Response) string {
	var output strings.Builder

	// Status line
	output.WriteString(fmt.Sprintf("\nHTTP/%d.%d %s\n",
		resp.ProtoMajor, resp.ProtoMinor, resp.Status))

	// Headers
	for key, values := range resp.Header {
		switch key {
		case "X-Forwarded-For", "User-Agent", "Accept-Encoding", "Content-Length",
			"Connection", "X-Envoy-Upstream-Service-Time", "Date",
			"X-Dc-Transaction-Id", "Strict-Transport-Security", "Pragma",
			"X-Frame-Options", "Cache-Control", "X-Xss-Protection",
			"X-Content-Type-Options", "Vary", "Expires":
			continue
		default:
		}
		for _, value := range values {
			output.WriteString(fmt.Sprintf("%s: %s\n", key, value))
		}
	}

	output.WriteString("\n") // Separate headers from body

	// Read response body
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		output.WriteString(fmt.Sprintf("Error reading body: %v\n", err))
	} else {
		if resp.Header.Get("Content-Type") == "application/json" {
			output.WriteString(maybePrettify(string(bodyBytes)))
		} else {
			output.WriteString(string(bodyBytes))
		}
	}

	// Reset the response body so it can be read again if needed
	resp.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	return output.String()
}

func (c *Curl) RoundTrip(req *http.Request) (*http.Response, error) {
	h := c.CurlFromRequest(req)
	var jarIdx int
	var jarFound bool
	if len(req.Cookies()) > 0 {
		jarIdx, jarFound = c.findMatchingJar(req.Cookies(), req.URL)
		if !jarFound {
			jarIdx = c.addJar()
			jarFound = true
		}
	}

	resp, err := c.Transport.RoundTrip(req)

	if len(resp.Cookies()) > 0 {
		if !jarFound {
			jarIdx = c.addJar()
			jarFound = true
		}
		c.updateJar(jarIdx, req.URL, resp.Cookies())
	}
	if jarFound {
		h.Args = append(h.Args, "--cookie-jar", fmt.Sprintf("jar-%d.txt", jarIdx))
	}
	h.Output = c.curlResponseFormat(resp)
	c.Listener.Notify(h)

	return resp, err
}
