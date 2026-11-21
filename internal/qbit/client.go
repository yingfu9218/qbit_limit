package qbit

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"qbit_limit/internal/config"
)

// TransferInfo mirrors qBittorrent /api/v2/transfer/info response fields.
type TransferInfo struct {
	DLInfoSpeed int64  `json:"dl_info_speed"`
	DLInfoData  int64  `json:"dl_info_data"`
	UPInfoSpeed int64  `json:"up_info_speed"`
	UPInfoData  int64  `json:"up_info_data"`
	DLRateLimit int64  `json:"dl_rate_limit"`
	UPRateLimit int64  `json:"up_rate_limit"`
	DHTNodes    int64  `json:"dht_nodes"`
	ConnStatus  string `json:"connection_status"`
}

// TestResult is returned by TestConnection.
type TestResult struct {
	OK         bool   `json:"ok"`
	AppVersion string `json:"app_version"`
	APIVersion string `json:"api_version"`
	LatencyMs  int64  `json:"latency_ms"`
	Error      string `json:"error,omitempty"`
}

// Client is a thread-safe qBittorrent WebUI API v2 client with automatic session re-login.
type Client struct {
	mu       sync.Mutex
	host     string
	username string
	password string
	http     *http.Client
	loggedIn bool
}

func NewClient(host, username, password string) *Client {
	jar, _ := cookiejar.New(nil)
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // common on self-signed NAS WebUI
	}
	return &Client{
		host:     config.NormalizeHost(host),
		username: username,
		password: password,
		http: &http.Client{
			Jar:       jar,
			Timeout:   10 * time.Second,
			Transport: transport,
		},
	}
}

// SetTransport overrides the underlying HTTP RoundTripper (useful for unit tests).
func (c *Client) SetTransport(rt http.RoundTripper) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.http.Transport = rt
}

// Configure updates credentials if changed and resets session state.
func (c *Client) Configure(host, username, password string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	norm := config.NormalizeHost(host)
	if norm != c.host || username != c.username || password != c.password {
		c.host = norm
		c.username = username
		c.password = password
		c.loggedIn = false
		jar, _ := cookiejar.New(nil)
		c.http.Jar = jar
	}
}

func (c *Client) loginLocked(ctx context.Context) error {
	if c.host == "" {
		return fmt.Errorf("未配置 qBittorrent 服务地址")
	}
	form := url.Values{}
	form.Set("username", c.username)
	form.Set("password", c.password)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.host+"/api/v2/auth/login", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Referer", c.host)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("连接 qBittorrent 失败: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	text := strings.TrimSpace(string(body))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("登录失败 (HTTP %d): %s", resp.StatusCode, text)
	}
	if text != "Ok." {
		return fmt.Errorf("认证失败，请检查用户名或密码 (返回: %s)", text)
	}
	c.loggedIn = true
	return nil
}

func (c *Client) doWithAuthLocked(ctx context.Context, method, path string, form url.Values) ([]byte, error) {
	if !c.loggedIn {
		if err := c.loginLocked(ctx); err != nil {
			return nil, err
		}
	}

	execOnce := func() (int, []byte, error) {
		var bodyReader io.Reader
		if form != nil {
			bodyReader = strings.NewReader(form.Encode())
		}
		req, err := http.NewRequestWithContext(ctx, method, c.host+path, bodyReader)
		if err != nil {
			return 0, nil, err
		}
		if form != nil {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		req.Header.Set("Referer", c.host)
		resp, err := c.http.Do(req)
		if err != nil {
			return 0, nil, err
		}
		defer resp.Body.Close()
		b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return resp.StatusCode, b, err
	}

	status, body, err := execOnce()
	if err != nil {
		c.loggedIn = false
		return nil, err
	}
	if status == http.StatusForbidden {
		// Session expired; re-authenticate once and retry.
		c.loggedIn = false
		if err := c.loginLocked(ctx); err != nil {
			return nil, err
		}
		status, body, err = execOnce()
		if err != nil {
			return nil, err
		}
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("qBittorrent API %s 返回 HTTP %d: %s", path, status, strings.TrimSpace(string(body)))
	}
	return body, nil
}

// GetTransferInfo retrieves current transfer speeds, cumulative session bytes, and rate limits.
func (c *Client) GetTransferInfo(ctx context.Context) (TransferInfo, int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	start := time.Now()
	body, err := c.doWithAuthLocked(ctx, http.MethodGet, "/api/v2/transfer/info", nil)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return TransferInfo{}, latency, err
	}
	var info TransferInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return TransferInfo{}, latency, fmt.Errorf("解析 transfer/info 失败: %w", err)
	}
	return info, latency, nil
}

// SetUploadLimit sets global upload speed limit in bytes/sec (0 = unlimited).
func (c *Client) SetUploadLimit(ctx context.Context, limitBytesPerSec int64) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	form := url.Values{}
	form.Set("limit", strconv.FormatInt(limitBytesPerSec, 10))
	_, err := c.doWithAuthLocked(ctx, http.MethodPost, "/api/v2/transfer/setUploadLimit", form)
	return err
}

// GetVersion fetches qBittorrent application version and WebAPI version.
func (c *Client) GetVersion(ctx context.Context) (appVer string, apiVer string, latencyMs int64, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	start := time.Now()
	b1, err := c.doWithAuthLocked(ctx, http.MethodGet, "/api/v2/app/version", nil)
	if err != nil {
		return "", "", time.Since(start).Milliseconds(), err
	}
	b2, err := c.doWithAuthLocked(ctx, http.MethodGet, "/api/v2/app/webapiVersion", nil)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return strings.TrimSpace(string(b1)), "", latency, nil
	}
	return strings.TrimSpace(string(b1)), strings.TrimSpace(string(b2)), latency, nil
}

// TestConnection creates an ephemeral client to validate settings before saving.
func TestConnection(ctx context.Context, host, username, password string) TestResult {
	tmp := NewClient(host, username, password)
	appVer, apiVer, latency, err := tmp.GetVersion(ctx)
	if err != nil {
		return TestResult{
			OK:        false,
			LatencyMs: latency,
			Error:     err.Error(),
		}
	}
	return TestResult{
		OK:         true,
		AppVersion: appVer,
		APIVersion: apiVer,
		LatencyMs:  latency,
	}
}
