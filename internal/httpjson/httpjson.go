// Package httpjson sends read-only JSON requests over HTTP: JSON-RPC 2.0
// calls and GET requests. Node types use it to query their nodes.
//
// Endpoint URLs are treated as secrets: RPC providers often put API keys into
// the path, query or subdomain. No error returned by this package contains
// the URL or any part of it.
package httpjson

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

// maxResponseBytes limits how much of a response body is read. Version
// responses are tiny; the limit protects against misbehaving endpoints.
const maxResponseBytes = 1 << 20 // 1 MiB

// Client sends JSON requests over HTTP.
//
// The zero value is not usable; create one with NewClient.
type Client struct {
	http *http.Client
}

// NewClient returns a Client that uses httpClient for requests. Pass nil to
// use a default client.
//
// Timeouts are controlled per call through the context, so httpClient does
// not need its own Timeout.
func NewClient(httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	// Copy the struct so that changing CheckRedirect does not affect the
	// caller's client.
	c := *httpClient
	// Do not follow redirects. A JSON-RPC or beacon endpoint has no reason
	// to redirect, and following one would send the request (and possibly
	// credentials in the URL) somewhere the user did not configure.
	c.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &Client{http: &c}
}

// rpcRequest and rpcResponse follow the JSON-RPC 2.0 specification.
type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Method  string `json:"method"`
	Params  []any  `json:"params"`
}

type rpcResponse struct {
	// json.RawMessage keeps the "result" bytes undecoded, so we can first
	// check for an error and then decode the result into the caller's type.
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
}

type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

// Call sends a JSON-RPC 2.0 request for method with the given parameters
// (none: an empty list) and decodes the result into result. params is
// variadic ("...any"): callers without parameters simply leave it out.
func (c *Client) Call(ctx context.Context, endpoint, method string, result any, params ...any) error {
	u, err := parseEndpoint(endpoint)
	if err != nil {
		return err
	}

	if params == nil {
		// JSON-RPC wants a list; a nil slice would be encoded as null.
		params = []any{}
	}
	body, err := json.Marshal(rpcRequest{JSONRPC: "2.0", ID: 1, Method: method, Params: params})
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(body))
	if err != nil {
		return redact(err, u)
	}
	req.Header.Set("Content-Type", "application/json")

	var resp rpcResponse
	// Some servers answer a JSON-RPC error with HTTP 400 instead of 200,
	// e.g. Aztec nodes (http200OnError: false); the error object is in
	// the body either way.
	status, err := c.do(req, u, &resp, http.StatusBadRequest)
	if err != nil {
		return err
	}
	if resp.Error != nil {
		// Data is kept only if it is a JSON string, such as the revert
		// data of eth_call ("0x..."); it is never part of the message.
		var data string
		json.Unmarshal(resp.Error.Data, &data)
		// The message comes from the server; redact it in case the server
		// echoes the request URL.
		return &RPCError{Code: resp.Error.Code, Data: data,
			msg: redact(fmt.Errorf("JSON-RPC error %d: %s", resp.Error.Code, resp.Error.Message), u).Error()}
	}
	if status != http.StatusOK {
		return statusError(status)
	}
	// A missing result and an explicit null are both invalid here; decoding
	// null would silently leave result at its zero value (e.g. false).
	if len(resp.Result) == 0 || string(resp.Result) == "null" {
		return errors.New("JSON-RPC response has neither result nor error")
	}
	if err := json.Unmarshal(resp.Result, result); err != nil {
		return fmt.Errorf("decode result: %w", err)
	}
	return nil
}

// Get sends a GET request for path below endpoint and decodes the JSON
// answer into out.
func (c *Client) Get(ctx context.Context, endpoint, path string, out any) error {
	base, err := parseEndpoint(endpoint)
	if err != nil {
		return err
	}
	// JoinPath keeps an existing path prefix and query string, so provider
	// URLs like https://host/<key>/ still work.
	full := base.JoinPath(path)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, full.String(), nil)
	if err != nil {
		return redact(err, full)
	}
	req.Header.Set("Accept", "application/json")
	// Redact based on the configured URL, not the full one: the API path
	// (e.g. "eth/v1/node/version") is public and should stay readable in errors.
	// The full URL is still covered: host, configured path segments and
	// query values are each redacted on their own.
	_, err = c.do(req, base, out)
	return err
}

// do sends req and decodes a JSON response into out. It returns the HTTP
// status. Only 200 and the statuses in also are decoded; for those in also,
// a body that is not JSON gives the status error. All errors are redacted.
func (c *Client) do(req *http.Request, u *url.URL, out any, also ...int) (int, error) {
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, &TransportError{err: redact(describeTransportError(req.Context(), err), u)}
	}
	// defer runs when do returns, whichever return statement is taken.
	defer resp.Body.Close()

	ok := resp.StatusCode == http.StatusOK
	if !ok && !slices.Contains(also, resp.StatusCode) {
		// The body is deliberately not included: provider error pages may
		// contain the API key or other details we do not want in logs.
		return resp.StatusCode, statusError(resp.StatusCode)
	}

	dec := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes))
	if err := dec.Decode(out); err != nil {
		if !ok {
			return resp.StatusCode, statusError(resp.StatusCode)
		}
		return resp.StatusCode, redact(fmt.Errorf("decode response: %w", err), u)
	}
	return resp.StatusCode, nil
}

func statusError(code int) error {
	return fmt.Errorf("unexpected HTTP status %s", StatusText(code))
}

// RPCError is an error object a JSON-RPC server returned, e.g. code -32601
// "method not found". Its message comes from the server and is redacted
// like every error of this package.
type RPCError struct {
	Code int
	// Data is the error's "data" if it is a string, e.g. the revert data
	// of an eth_call; "" otherwise. It is not part of Error().
	Data string
	msg  string
}

func (e *RPCError) Error() string { return e.msg }

// RPCErrorCode returns the code of the RPCError in err's chain, if any.
func RPCErrorCode(err error) (int, bool) {
	var re *RPCError
	if errors.As(err, &re) {
		return re.Code, true
	}
	return 0, false
}

// TransportError means the endpoint could not be reached or did not answer
// in time: timeout, connection failure, DNS failure, cancellation. Errors
// the server reported (HTTP status, JSON-RPC error, malformed response) are
// not TransportErrors.
//
// Its message is already redacted.
type TransportError struct {
	err error
}

func (e *TransportError) Error() string { return e.err.Error() }

// Unwrap lets errors.Is/As look inside. The wrapped error is the redacted
// one, so this does not expose the URL.
func (e *TransportError) Unwrap() error { return e.err }

// IsTransportError reports whether err (or any error it wraps) is a
// TransportError.
func IsTransportError(err error) bool {
	var te *TransportError
	return errors.As(err, &te)
}

// StatusText formats an HTTP status as "500 Internal Server Error", or just
// "525" for codes without a standard text. The reason phrase sent by the
// server is ignored: it varies ("525 " from Cloudflare, "525 status code
// 525" from Go servers) and is not under our control.
func StatusText(code int) string {
	if text := http.StatusText(code); text != "" {
		return fmt.Sprintf("%d %s", code, text)
	}
	return strconv.Itoa(code)
}

// describeTransportError turns errors from http.Client.Do into short,
// readable messages. It removes the *url.Error wrapper, whose message always
// contains the full request URL.
func describeTransportError(ctx context.Context, err error) error {
	// errors.Is walks the whole chain of wrapped errors.
	if errors.Is(err, context.DeadlineExceeded) {
		return errors.New("timeout")
	}
	if errors.Is(err, context.Canceled) {
		return errors.New("canceled")
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}

	// errors.As finds the first error in the chain with the given type and
	// stores it in the variable.
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		if dnsErr.IsNotFound {
			return errors.New("DNS lookup failed: host not found")
		}
		return errors.New("DNS lookup failed")
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return errors.New("timeout")
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) && opErr.Op == "dial" {
		// opErr.Err is e.g. "connect: connection refused", without address.
		return fmt.Errorf("connection failed: %v", opErr.Err)
	}

	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return fmt.Errorf("request failed: %v", urlErr.Err)
	}
	return fmt.Errorf("request failed: %v", err)
}

// parseEndpoint parses a configured endpoint URL without leaking it in errors.
func parseEndpoint(endpoint string) (*url.URL, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, errors.New("invalid endpoint URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, errors.New("invalid endpoint URL: scheme must be http or https")
	}
	return u, nil
}

// redact removes every sensitive part of u from err's message: the full URL,
// host, path segments, query and user info. It is a last line of defence
// for messages we do not control (e.g. from the TLS stack or the server).
//
// The returned error no longer wraps err, so errors.Is/As cannot reach the
// original (which may contain the URL).
func redact(err error, u *url.URL) error {
	if err == nil || u == nil {
		return err
	}
	msg := err.Error()

	var secrets []string
	add := func(s string) {
		if len(s) >= 3 { // avoid replacing trivial strings such as "v1"
			secrets = append(secrets, s)
		}
	}
	add(u.String())
	add(u.Redacted())
	add(u.Host)
	add(u.Hostname())
	add(u.RawQuery)
	if u.User != nil {
		add(u.User.String())
		add(u.User.Username())
		if p, ok := u.User.Password(); ok {
			add(p)
		}
	}
	add(strings.Trim(u.EscapedPath(), "/"))
	for _, seg := range strings.Split(u.EscapedPath(), "/") {
		add(seg)
	}
	for _, values := range u.Query() {
		for _, v := range values {
			add(v)
		}
	}
	// Longest first, so the full URL is replaced before its parts.
	slices.SortFunc(secrets, func(a, b string) int { return len(b) - len(a) })
	for _, s := range secrets {
		msg = strings.ReplaceAll(msg, s, "[redacted]")
	}
	return errors.New(msg)
}
