// Package s3client provides a pure Go standard-library S3 client with AWS SigV4
// for ctty's S3 / Object Storage TUI and CLI without bulky external dependencies.
package s3client

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/zsuroy/ctty/internal/s3config"
	"github.com/zsuroy/ctty/internal/s3cred"
)

// RemoteEntry represents a remote S3 bucket, prefix (virtual directory), or object.
type RemoteEntry struct {
	Name    string    `json:"name"`
	IsDir   bool      `json:"is_dir"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"mod_time"`
	ETag    string    `json:"etag,omitempty"`
	Key     string    `json:"key"` // full object key or bucket name
}

// Client wraps an HTTP client and AWS SigV4 signer for S3 operations.
type Client struct {
	httpClient *http.Client
	site       s3config.S3Site
	secretKey  string
	endpoint   string
	useSSL     bool
	mu         sync.Mutex
}

// Site returns the site configuration used by this client.
func (c *Client) Site() s3config.S3Site {
	return c.site
}

// Connect creates and validates an S3 client connection for the given site.
// If secretKey is empty, it attempts to load a saved secret key from the vault.
func Connect(site s3config.S3Site, secretKey string) (*Client, error) {
	if secretKey == "" {
		if saved, ok := s3cred.GetSecretKey(site.Name); ok {
			secretKey = saved
		}
	}

	rawEndpoint := strings.TrimSpace(site.Endpoint)
	if rawEndpoint == "" {
		rawEndpoint = "s3.amazonaws.com"
	}

	useSSL := site.UseSSL
	if strings.HasPrefix(rawEndpoint, "http://") {
		useSSL = false
		rawEndpoint = strings.TrimPrefix(rawEndpoint, "http://")
	} else if strings.HasPrefix(rawEndpoint, "https://") {
		useSSL = true
		rawEndpoint = strings.TrimPrefix(rawEndpoint, "https://")
	}
	rawEndpoint = strings.TrimRight(rawEndpoint, "/")

	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   15 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	if site.InsecureTLS {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}

	httpClient := &http.Client{
		Transport: transport,
		Timeout:   0, // per-request contexts control timeouts
	}

	return &Client{
		httpClient: httpClient,
		site:       site,
		secretKey:  secretKey,
		endpoint:   rawEndpoint,
		useSSL:     useSSL,
	}, nil
}

// Probe checks if an S3 site is reachable within the given timeout.
func Probe(site s3config.S3Site, timeout time.Duration) error {
	secretKey, _ := s3cred.GetSecretKey(site.Name)
	client, err := Connect(site, secretKey)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if site.Bucket != "" {
		req, err := client.newRequest(ctx, http.MethodHead, site.Bucket, "", nil, nil)
		if err != nil {
			return err
		}
		resp, err := client.httpClient.Do(req)
		if err != nil {
			return err
		}
		_ = resp.Body.Close()
		if resp.StatusCode >= 400 && resp.StatusCode != http.StatusForbidden && resp.StatusCode != http.StatusMethodNotAllowed {
			return fmt.Errorf("probe bucket %s failed: status %d", site.Bucket, resp.StatusCode)
		}
		return nil
	}

	req, err := client.newRequest(ctx, http.MethodGet, "", "", nil, nil)
	if err != nil {
		return err
	}
	resp, err := client.httpClient.Do(req)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	if resp.StatusCode >= 400 && resp.StatusCode != http.StatusForbidden {
		return fmt.Errorf("probe failed: status %d", resp.StatusCode)
	}
	return nil
}

// ResolveBucketAndPrefix parses a remote path into bucket and object prefix.
func (c *Client) ResolveBucketAndPrefix(remotePath string) (bucket string, prefix string) {
	clean := strings.Trim(strings.TrimSpace(remotePath), "/")
	if c.site.Bucket != "" {
		if clean == "" || clean == "." {
			return c.site.Bucket, ""
		}
		if clean == c.site.Bucket {
			return c.site.Bucket, ""
		}
		if strings.HasPrefix(clean, c.site.Bucket+"/") {
			clean = strings.TrimPrefix(clean, c.site.Bucket+"/")
		}
		return c.site.Bucket, clean
	}

	if clean == "" || clean == "." {
		return "", ""
	}
	parts := strings.SplitN(clean, "/", 2)
	bucket = parts[0]
	if len(parts) > 1 {
		prefix = parts[1]
	}
	return bucket, prefix
}

func isIPOrPort(host string) bool {
	h := strings.TrimSpace(host)
	if strings.Contains(h, ":") {
		return true // Host has an explicit port (e.g. host:port) or is an IPv6 address
	}
	return net.ParseIP(h) != nil || h == "localhost"
}

func isAWSEndpoint(endpoint string) bool {
	h := strings.ToLower(strings.TrimSpace(endpoint))
	if hostPart, _, err := net.SplitHostPort(h); err == nil {
		h = hostPart
	}
	return h == "s3.amazonaws.com" ||
		strings.HasSuffix(h, ".amazonaws.com") ||
		strings.HasSuffix(h, ".amazonaws.com.cn")
}

func (c *Client) shouldUsePathStyle(bucket string) bool {
	if c.site.PathStyle {
		return true
	}
	if bucket == "" {
		return true
	}
	if strings.Contains(bucket, ".") {
		return true
	}
	if isIPOrPort(c.endpoint) {
		return true
	}
	if !isAWSEndpoint(c.endpoint) {
		return true
	}
	return false
}

func (c *Client) buildURL(bucket, key string, query url.Values) (*url.URL, error) {
	scheme := "https"
	if !c.useSSL {
		scheme = "http"
	}

	usePathStyle := c.shouldUsePathStyle(bucket)
	var host, reqPath string

	if usePathStyle {
		host = c.endpoint
		if bucket != "" {
			reqPath = "/" + bucket
			if key != "" {
				if !strings.HasPrefix(key, "/") {
					reqPath += "/"
				}
				reqPath += key
			}
		} else {
			reqPath = "/"
		}
	} else {
		host = bucket + "." + c.endpoint
		if key != "" {
			if !strings.HasPrefix(key, "/") {
				reqPath = "/" + key
			} else {
				reqPath = key
			}
		} else {
			reqPath = "/"
		}
	}

	u := &url.URL{
		Scheme:   scheme,
		Host:     host,
		Path:     reqPath,
		RawQuery: query.Encode(),
	}
	return u, nil
}

const emptyPayloadSHA256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

func (c *Client) newSignedRequestWithHeaders(ctx context.Context, method, bucket, key string, query url.Values, body io.Reader, contentType string, contentLength int64, payloadSHA256 string, extraHeaders map[string]string) (*http.Request, error) {
	u, err := c.buildURL(bucket, key, query)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, err
	}

	req.Host = u.Host
	if contentLength > 0 || (contentLength == 0 && body != nil && method == http.MethodPut) {
		req.ContentLength = contentLength
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}

	if payloadSHA256 == "" {
		if body == nil || contentLength == 0 {
			payloadSHA256 = emptyPayloadSHA256
		} else {
			payloadSHA256 = "UNSIGNED-PAYLOAD"
		}
	}

	c.signRequest(req, payloadSHA256)
	return req, nil
}

func (c *Client) newSignedRequest(ctx context.Context, method, bucket, key string, query url.Values, body io.Reader, contentType string, contentLength int64, payloadSHA256 string) (*http.Request, error) {
	return c.newSignedRequestWithHeaders(ctx, method, bucket, key, query, body, contentType, contentLength, payloadSHA256, nil)
}

func (c *Client) newRequest(ctx context.Context, method, bucket, key string, query url.Values, body io.Reader) (*http.Request, error) {
	return c.newSignedRequest(ctx, method, bucket, key, query, body, "", 0, "")
}

func uriEncode(s string) string {
	return strings.ReplaceAll(url.QueryEscape(s), "+", "%20")
}

func canonicalQueryString(values url.Values) string {
	if len(values) == 0 {
		return ""
	}
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var parts []string
	for _, k := range keys {
		vals := values[k]
		sort.Strings(vals)
		ek := uriEncode(k)
		if len(vals) == 0 {
			parts = append(parts, ek+"=")
		} else {
			for _, v := range vals {
				parts = append(parts, ek+"="+uriEncode(v))
			}
		}
	}
	return strings.Join(parts, "&")
}

func canonicalURI(u *url.URL) string {
	p := u.Path
	if p == "" {
		return "/"
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	segments := strings.Split(p, "/")
	for i, seg := range segments {
		segments[i] = uriEncode(seg)
	}
	return strings.Join(segments, "/")
}

func (c *Client) signRequest(req *http.Request, payloadSHA256 string) {
	if c.site.AccessKey == "" && c.secretKey == "" {
		return // Anonymous access
	}

	t := time.Now().UTC()
	date := t.Format("20060102")
	amzDate := t.Format("20060102T150405Z")

	req.Header.Set("x-amz-date", amzDate)
	if payloadSHA256 == "" {
		payloadSHA256 = "UNSIGNED-PAYLOAD"
	}
	req.Header.Set("x-amz-content-sha256", payloadSHA256)

	region := c.site.Region
	if region == "" {
		region = "us-east-1"
	}

	hMap := map[string]string{
		"host":                 req.Host,
		"x-amz-content-sha256": payloadSHA256,
		"x-amz-date":           amzDate,
	}
	for k, vv := range req.Header {
		lk := strings.ToLower(k)
		if strings.HasPrefix(lk, "x-amz-") || lk == "content-type" {
			hMap[lk] = strings.TrimSpace(strings.Join(vv, ","))
		}
	}

	var headerKeys []string
	for k := range hMap {
		headerKeys = append(headerKeys, k)
	}
	sort.Strings(headerKeys)

	var canonicalHeaders strings.Builder
	for _, k := range headerKeys {
		canonicalHeaders.WriteString(k)
		canonicalHeaders.WriteString(":")
		canonicalHeaders.WriteString(strings.TrimSpace(hMap[k]))
		canonicalHeaders.WriteString("\n")
	}
	signedHeaders := strings.Join(headerKeys, ";")

	canURI := canonicalURI(req.URL)
	canQuery := canonicalQueryString(req.URL.Query())

	canonicalReq := fmt.Sprintf("%s\n%s\n%s\n%s\n%s\n%s",
		req.Method,
		canURI,
		canQuery,
		canonicalHeaders.String(),
		signedHeaders,
		payloadSHA256,
	)

	reqHash := sha256Hex([]byte(canonicalReq))
	scope := fmt.Sprintf("%s/%s/s3/aws4_request", date, region)
	stringToSign := fmt.Sprintf("AWS4-HMAC-SHA256\n%s\n%s\n%s", amzDate, scope, reqHash)

	kDate := hmacSHA256([]byte("AWS4"+c.secretKey), []byte(date))
	kRegion := hmacSHA256(kDate, []byte(region))
	kService := hmacSHA256(kRegion, []byte("s3"))
	kSigning := hmacSHA256(kService, []byte("aws4_request"))
	signature := hex.EncodeToString(hmacSHA256(kSigning, []byte(stringToSign)))

	auth := fmt.Sprintf("AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		c.site.AccessKey, scope, signedHeaders, signature)
	req.Header.Set("Authorization", auth)
}

func hmacSHA256(key, data []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return h.Sum(nil)
}

func sha256Hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

type s3ErrorResponse struct {
	XMLName xml.Name `xml:"Error"`
	Code    string   `xml:"Code"`
	Message string   `xml:"Message"`
}

func parseS3Error(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if len(body) > 0 {
		var s3Err s3ErrorResponse
		if err := xml.Unmarshal(body, &s3Err); err == nil && (s3Err.Code != "" || s3Err.Message != "") {
			return fmt.Errorf("s3 error: %s (%s)", s3Err.Message, s3Err.Code)
		}
	}
	return fmt.Errorf("s3 request failed (%d %s)", resp.StatusCode, resp.Status)
}

type listAllMyBucketsResult struct {
	XMLName xml.Name `xml:"ListAllMyBucketsResult"`
	Buckets struct {
		Bucket []struct {
			Name         string    `xml:"Name"`
			CreationDate time.Time `xml:"CreationDate"`
		} `xml:"Bucket"`
	} `xml:"Buckets"`
}

type listBucketResult struct {
	XMLName               xml.Name `xml:"ListBucketResult"`
	Name                  string   `xml:"Name"`
	Prefix                string   `xml:"Prefix"`
	IsTruncated           bool     `xml:"IsTruncated"`
	NextContinuationToken string   `xml:"NextContinuationToken"`
	Contents              []struct {
		Key          string    `xml:"Key"`
		Size         int64     `xml:"Size"`
		LastModified time.Time `xml:"LastModified"`
		ETag         string    `xml:"ETag"`
	} `xml:"Contents"`
	CommonPrefixes []struct {
		Prefix string `xml:"Prefix"`
	} `xml:"CommonPrefixes"`
}

// ListBuckets returns all buckets accessible with current credentials.
func (c *Client) ListBuckets(ctx context.Context) ([]RemoteEntry, error) {
	req, err := c.newRequest(ctx, http.MethodGet, "", "", nil, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, parseS3Error(resp)
	}

	var res listAllMyBucketsResult
	if err := xml.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, fmt.Errorf("failed to parse buckets XML: %w", err)
	}

	entries := make([]RemoteEntry, 0, len(res.Buckets.Bucket))
	for _, b := range res.Buckets.Bucket {
		entries = append(entries, RemoteEntry{
			Name:    b.Name,
			IsDir:   true,
			ModTime: b.CreationDate,
			Key:     b.Name,
		})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return entries, nil
}

// List returns directory entries (buckets or objects/prefixes) at remotePath.
func (c *Client) List(ctx context.Context, remotePath string) ([]RemoteEntry, error) {
	bucket, prefix := c.ResolveBucketAndPrefix(remotePath)
	if bucket == "" {
		return c.ListBuckets(ctx)
	}

	queryPrefix := prefix
	if queryPrefix != "" && !strings.HasSuffix(queryPrefix, "/") {
		queryPrefix += "/"
	}

	q := url.Values{
		"list-type": []string{"2"},
		"delimiter": []string{"/"},
	}
	if queryPrefix != "" {
		q.Set("prefix", queryPrefix)
	}

	var entries []RemoteEntry
	seenDirs := make(map[string]bool)

	for {
		req, err := c.newRequest(ctx, http.MethodGet, bucket, "", q, nil)
		if err != nil {
			return nil, err
		}
		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode >= 400 {
			_ = resp.Body.Close()
			return nil, parseS3Error(resp)
		}

		var res listBucketResult
		err = xml.NewDecoder(resp.Body).Decode(&res)
		_ = resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("failed to parse list XML: %w", err)
		}

		for _, cp := range res.CommonPrefixes {
			dirName := strings.TrimPrefix(cp.Prefix, queryPrefix)
			dirName = strings.Trim(dirName, "/")
			if dirName != "" && !seenDirs[dirName] {
				seenDirs[dirName] = true
				entries = append(entries, RemoteEntry{
					Name:  dirName,
					IsDir: true,
					Key:   strings.TrimSuffix(cp.Prefix, "/"),
				})
			}
		}

		for _, obj := range res.Contents {
			if obj.Key == queryPrefix || obj.Key == queryPrefix+"/" {
				continue
			}
			rel := strings.TrimPrefix(obj.Key, queryPrefix)
			rel = strings.TrimPrefix(rel, "/")
			if rel == "" {
				continue
			}
			isDir := strings.HasSuffix(obj.Key, "/")
			name := rel
			if isDir {
				name = strings.TrimSuffix(name, "/")
				if seenDirs[name] {
					continue
				}
				seenDirs[name] = true
			}
			entries = append(entries, RemoteEntry{
				Name:    name,
				IsDir:   isDir,
				Size:    obj.Size,
				ModTime: obj.LastModified,
				ETag:    obj.ETag,
				Key:     obj.Key,
			})
		}

		if res.IsTruncated && res.NextContinuationToken != "" {
			q.Set("continuation-token", res.NextContinuationToken)
		} else {
			break
		}
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir
		}
		return entries[i].Name < entries[j].Name
	})

	return entries, nil
}

// IsRemoteDir checks whether remotePath represents a bucket or prefix directory.
func (c *Client) IsRemoteDir(ctx context.Context, remotePath string) (bool, error) {
	bucket, prefix := c.ResolveBucketAndPrefix(remotePath)
	if bucket == "" || prefix == "" {
		return true, nil
	}
	if strings.HasSuffix(prefix, "/") {
		return true, nil
	}

	req, err := c.newRequest(ctx, http.MethodHead, bucket, prefix, nil, nil)
	if err == nil {
		resp, err := c.httpClient.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return false, nil
			}
		}
	}

	q := url.Values{
		"list-type": []string{"2"},
		"prefix":    []string{prefix + "/"},
		"max-keys":  []string{"1"},
	}
	req, err = c.newRequest(ctx, http.MethodGet, bucket, "", q, nil)
	if err != nil {
		return false, nil
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false, nil
	}
	defer resp.Body.Close()
	if resp.StatusCode < 400 {
		var res listBucketResult
		if err := xml.NewDecoder(resp.Body).Decode(&res); err == nil {
			if len(res.Contents) > 0 || len(res.CommonPrefixes) > 0 {
				return true, nil
			}
		}
	}
	return false, nil
}

type progressReader struct {
	r          io.Reader
	ctx        context.Context
	done       int64
	total      int64
	onProgress func(done, total int64)
}

func (pr *progressReader) Read(p []byte) (int, error) {
	select {
	case <-pr.ctx.Done():
		return 0, pr.ctx.Err()
	default:
	}

	n, err := pr.r.Read(p)
	if n > 0 {
		pr.done += int64(n)
		if pr.onProgress != nil {
			pr.onProgress(pr.done, pr.total)
		}
	}
	return n, err
}

// DownloadWithProgressCtx downloads a single object to localPath, reporting progress.
func (c *Client) DownloadWithProgressCtx(ctx context.Context, bucket, objectKey, localPath string, onProgress func(done, total int64)) error {
	req, err := c.newRequest(ctx, http.MethodGet, bucket, objectKey, nil, nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return parseS3Error(resp)
	}

	total := resp.ContentLength
	dir := filepath.Dir(localPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	tmp := localPath + ".ctty.tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}

	buf := make([]byte, 32*1024)
	var done int64
	for {
		select {
		case <-ctx.Done():
			_ = f.Close()
			_ = os.Remove(tmp)
			return ctx.Err()
		default:
		}

		n, rErr := resp.Body.Read(buf)
		if n > 0 {
			if _, wErr := f.Write(buf[:n]); wErr != nil {
				_ = f.Close()
				_ = os.Remove(tmp)
				return wErr
			}
			done += int64(n)
			if onProgress != nil {
				onProgress(done, total)
			}
		}
		if rErr != nil {
			if rErr == io.EOF {
				break
			}
			_ = f.Close()
			_ = os.Remove(tmp)
			return rErr
		}
	}

	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}

	return os.Rename(tmp, localPath)
}

// UploadWithProgressCtx uploads localPath to a single object in S3, reporting progress.
func (c *Client) UploadWithProgressCtx(ctx context.Context, localPath, bucket, objectKey string, onProgress func(done, total int64)) error {
	f, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return err
	}
	total := fi.Size()

	var payloadSHA256 string
	if total == 0 {
		payloadSHA256 = emptyPayloadSHA256
	} else {
		hasher := sha256.New()
		if _, err := io.Copy(hasher, f); err != nil {
			return err
		}
		payloadSHA256 = hex.EncodeToString(hasher.Sum(nil))
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return err
		}
	}

	pr := &progressReader{
		r:          f,
		ctx:        ctx,
		total:      total,
		onProgress: onProgress,
	}

	contentType := "application/octet-stream"
	if ext := filepath.Ext(localPath); ext != "" {
		if ct := mime.TypeByExtension(ext); ct != "" {
			contentType = ct
		}
	}

	req, err := c.newSignedRequest(ctx, http.MethodPut, bucket, objectKey, nil, pr, contentType, total, payloadSHA256)
	if err != nil {
		return err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return parseS3Error(resp)
	}
	return nil
}

// DownloadPath downloads remotePath (file or recursive prefix) to localPath.
func (c *Client) DownloadPath(ctx context.Context, remotePath, localPath string, onProgress func(done, total int64)) error {
	bucket, prefix := c.ResolveBucketAndPrefix(remotePath)
	if bucket == "" {
		return errors.New("cannot download: no bucket specified")
	}

	isDir, err := c.IsRemoteDir(ctx, remotePath)
	if err == nil && !isDir {
		return c.DownloadWithProgressCtx(ctx, bucket, prefix, localPath, onProgress)
	}

	queryPrefix := prefix
	if queryPrefix != "" && !strings.HasSuffix(queryPrefix, "/") {
		queryPrefix += "/"
	}

	if err := os.MkdirAll(localPath, 0755); err != nil {
		return err
	}

	q := url.Values{"list-type": []string{"2"}}
	if queryPrefix != "" {
		q.Set("prefix", queryPrefix)
	}

	for {
		req, err := c.newRequest(ctx, http.MethodGet, bucket, "", q, nil)
		if err != nil {
			return err
		}
		resp, err := c.httpClient.Do(req)
		if err != nil {
			return err
		}
		if resp.StatusCode >= 400 {
			_ = resp.Body.Close()
			return parseS3Error(resp)
		}

		var res listBucketResult
		err = xml.NewDecoder(resp.Body).Decode(&res)
		_ = resp.Body.Close()
		if err != nil {
			return fmt.Errorf("failed to parse list XML: %w", err)
		}

		for _, obj := range res.Contents {
			rel := strings.TrimPrefix(obj.Key, queryPrefix)
			if strings.HasSuffix(obj.Key, "/") {
				_ = os.MkdirAll(filepath.Join(localPath, filepath.FromSlash(rel)), 0755)
				continue
			}
			destFile := filepath.Join(localPath, filepath.FromSlash(rel))
			if err := c.DownloadWithProgressCtx(ctx, bucket, obj.Key, destFile, onProgress); err != nil {
				return err
			}
		}

		if res.IsTruncated && res.NextContinuationToken != "" {
			q.Set("continuation-token", res.NextContinuationToken)
		} else {
			break
		}
	}
	return nil
}

// UploadPath uploads localPath (file or recursive directory) to remotePath in S3.
func (c *Client) UploadPath(ctx context.Context, localPath, remotePath string, onProgress func(done, total int64)) error {
	bucket, prefix := c.ResolveBucketAndPrefix(remotePath)
	if bucket == "" {
		return errors.New("cannot upload: no bucket specified")
	}

	info, err := os.Stat(localPath)
	if err != nil {
		return err
	}

	if !info.IsDir() {
		targetKey := prefix
		if strings.HasSuffix(targetKey, "/") || targetKey == "" {
			targetKey = path.Join(targetKey, filepath.Base(localPath))
		}
		return c.UploadWithProgressCtx(ctx, localPath, bucket, targetKey, onProgress)
	}

	baseDir := filepath.Clean(localPath)
	return filepath.Walk(baseDir, func(filePath string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(baseDir, filePath)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		relSlash := filepath.ToSlash(rel)
		targetKey := path.Join(prefix, relSlash)

		if fi.IsDir() {
			markerKey := targetKey + "/"
			req, pErr := c.newSignedRequest(ctx, http.MethodPut, bucket, markerKey, nil, bytes.NewReader(nil), "application/x-directory", 0, emptyPayloadSHA256)
			if pErr != nil {
				return pErr
			}
			resp, pErr := c.httpClient.Do(req)
			if pErr != nil {
				return pErr
			}
			_ = resp.Body.Close()
			return nil
		}
		return c.UploadWithProgressCtx(ctx, filePath, bucket, targetKey, onProgress)
	})
}

// Mkdir creates a directory marker prefix or a bucket in S3.
func (c *Client) Mkdir(ctx context.Context, remotePath string) error {
	bucket, prefix := c.ResolveBucketAndPrefix(remotePath)
	if bucket == "" {
		return errors.New("bucket name required")
	}
	if prefix == "" {
		req, err := c.newSignedRequest(ctx, http.MethodPut, bucket, "", nil, bytes.NewReader(nil), "", 0, emptyPayloadSHA256)
		if err != nil {
			return err
		}
		resp, err := c.httpClient.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 400 && resp.StatusCode != http.StatusConflict {
			return parseS3Error(resp)
		}
		return nil
	}

	markerKey := strings.Trim(prefix, "/") + "/"
	req, err := c.newSignedRequest(ctx, http.MethodPut, bucket, markerKey, nil, bytes.NewReader(nil), "application/x-directory", 0, emptyPayloadSHA256)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return parseS3Error(resp)
	}
	return nil
}

// DeleteObject deletes an object by exact bucket and key.
func (c *Client) DeleteObject(ctx context.Context, bucket, key string) error {
	if bucket == "" {
		return errors.New("cannot delete: no bucket specified")
	}
	req, err := c.newRequest(ctx, http.MethodDelete, bucket, key, nil, nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 && resp.StatusCode != http.StatusNotFound {
		return parseS3Error(resp)
	}
	return nil
}

// Delete removes an object, empty bucket, or prefix.
func (c *Client) Delete(ctx context.Context, remotePath string) error {
	bucket, prefix := c.ResolveBucketAndPrefix(remotePath)
	if bucket == "" {
		return errors.New("cannot delete: no bucket specified")
	}
	if strings.HasSuffix(strings.TrimSpace(remotePath), "/") && prefix != "" && !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	return c.DeleteObject(ctx, bucket, prefix)
}

// DeletePrefix recursively deletes all objects matching the prefix.
func (c *Client) DeletePrefix(ctx context.Context, remotePath string) error {
	bucket, prefix := c.ResolveBucketAndPrefix(remotePath)
	if bucket == "" {
		return errors.New("cannot delete: no bucket specified")
	}

	queryPrefix := prefix
	if queryPrefix != "" && !strings.HasSuffix(queryPrefix, "/") {
		queryPrefix += "/"
	}

	q := url.Values{"list-type": []string{"2"}}
	if queryPrefix != "" {
		q.Set("prefix", queryPrefix)
	}

	var keysToDelete []string
	for {
		req, err := c.newRequest(ctx, http.MethodGet, bucket, "", q, nil)
		if err != nil {
			return err
		}
		resp, err := c.httpClient.Do(req)
		if err != nil {
			return err
		}
		if resp.StatusCode >= 400 {
			_ = resp.Body.Close()
			return parseS3Error(resp)
		}

		var res listBucketResult
		err = xml.NewDecoder(resp.Body).Decode(&res)
		_ = resp.Body.Close()
		if err != nil {
			return fmt.Errorf("failed to parse list XML: %w", err)
		}

		for _, item := range res.Contents {
			keysToDelete = append(keysToDelete, item.Key)
		}

		if res.IsTruncated && res.NextContinuationToken != "" {
			q.Set("continuation-token", res.NextContinuationToken)
		} else {
			break
		}
	}

	for _, key := range keysToDelete {
		if err := c.DeleteObject(ctx, bucket, key); err != nil {
			return fmt.Errorf("failed to delete %s: %w", key, err)
		}
	}
	if prefix != "" {
		_ = c.DeleteObject(ctx, bucket, prefix+"/")
		_ = c.DeleteObject(ctx, bucket, prefix)
	}
	return nil
}

// formatCopySource formats a canonical copy source string for x-amz-copy-source.
func formatCopySource(srcBucket, srcKey string) string {
	cleanKey := strings.TrimPrefix(srcKey, "/")
	parts := strings.Split(cleanKey, "/")
	encodedParts := make([]string, len(parts))
	for i, p := range parts {
		encodedParts[i] = uriEncode(p)
	}
	return "/" + uriEncode(srcBucket) + "/" + strings.Join(encodedParts, "/")
}

// CopyObject copies an object from srcBucket/srcKey to destBucket/destKey in S3.
func (c *Client) CopyObject(ctx context.Context, srcBucket, srcKey, destBucket, destKey string) error {
	copySource := formatCopySource(srcBucket, srcKey)
	headers := map[string]string{
		"x-amz-copy-source": copySource,
	}
	req, err := c.newSignedRequestWithHeaders(ctx, http.MethodPut, destBucket, destKey, nil, bytes.NewReader(nil), "", 0, emptyPayloadSHA256, headers)
	if err != nil {
		return err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return parseS3Error(resp)
	}

	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err == nil && bytes.Contains(bodyBytes, []byte("<Error>")) {
		var s3Err s3ErrorResponse
		if xmlErr := xml.Unmarshal(bodyBytes, &s3Err); xmlErr == nil && (s3Err.Code != "" || s3Err.Message != "") {
			return fmt.Errorf("s3 error: %s (%s)", s3Err.Message, s3Err.Code)
		}
	}
	return nil
}

// Rename renames a remote object or directory prefix in S3.
func (c *Client) Rename(ctx context.Context, oldRemotePath, newRemotePath string) error {
	srcBucket, srcKey := c.ResolveBucketAndPrefix(oldRemotePath)
	if srcBucket == "" {
		return errors.New("cannot rename: source bucket required")
	}
	if srcKey == "" {
		return errors.New("cannot rename bucket: S3 does not support bucket rename")
	}

	var destBucket, destKey string
	if !strings.Contains(newRemotePath, "/") {
		destBucket = srcBucket
		destDir := path.Dir(srcKey)
		if destDir == "." {
			destKey = newRemotePath
		} else {
			destKey = path.Join(destDir, newRemotePath)
		}
	} else {
		destBucket, destKey = c.ResolveBucketAndPrefix(newRemotePath)
	}
	if destBucket == "" {
		destBucket = srcBucket
	}
	if destKey == "" {
		return errors.New("cannot rename: destination object key required")
	}

	if srcBucket == destBucket && srcKey == destKey {
		return nil
	}

	isDir, err := c.IsRemoteDir(ctx, oldRemotePath)
	if err == nil && isDir {
		return c.RenamePrefix(ctx, oldRemotePath, newRemotePath)
	}

	if err := c.CopyObject(ctx, srcBucket, srcKey, destBucket, destKey); err != nil {
		return err
	}
	if err := c.DeleteObject(ctx, srcBucket, srcKey); err != nil {
		return fmt.Errorf("failed to delete old object %s: %w", srcKey, err)
	}
	return nil
}

// RenamePrefix recursively copies and deletes all objects matching the prefix.
func (c *Client) RenamePrefix(ctx context.Context, oldRemotePath, newRemotePath string) error {
	srcBucket, srcPrefix := c.ResolveBucketAndPrefix(oldRemotePath)
	if srcBucket == "" {
		return errors.New("cannot rename: source bucket required")
	}
	if srcPrefix == "" {
		return errors.New("cannot rename bucket: S3 does not support bucket rename")
	}

	var destBucket, destPrefix string
	if !strings.Contains(newRemotePath, "/") {
		destBucket = srcBucket
		destDir := path.Dir(srcPrefix)
		if destDir == "." {
			destPrefix = newRemotePath
		} else {
			destPrefix = path.Join(destDir, newRemotePath)
		}
	} else {
		destBucket, destPrefix = c.ResolveBucketAndPrefix(newRemotePath)
	}
	if destBucket == "" {
		destBucket = srcBucket
	}
	if destPrefix == "" {
		return errors.New("cannot rename: destination prefix required")
	}

	cleanSrcPrefix := strings.Trim(srcPrefix, "/")
	cleanDestPrefix := strings.Trim(destPrefix, "/")

	if srcBucket == destBucket && cleanSrcPrefix == cleanDestPrefix {
		return nil
	}

	queryPrefix := cleanSrcPrefix + "/"
	q := url.Values{"list-type": []string{"2"}, "prefix": []string{queryPrefix}}

	type copyItem struct {
		srcKey  string
		destKey string
	}
	var items []copyItem

	for {
		req, err := c.newRequest(ctx, http.MethodGet, srcBucket, "", q, nil)
		if err != nil {
			return err
		}
		resp, err := c.httpClient.Do(req)
		if err != nil {
			return err
		}
		if resp.StatusCode >= 400 {
			_ = resp.Body.Close()
			return parseS3Error(resp)
		}

		var res listBucketResult
		err = xml.NewDecoder(resp.Body).Decode(&res)
		_ = resp.Body.Close()
		if err != nil {
			return fmt.Errorf("failed to parse list XML: %w", err)
		}

		for _, item := range res.Contents {
			relKey := strings.TrimPrefix(item.Key, queryPrefix)
			var destKey string
			if relKey == "" {
				destKey = cleanDestPrefix + "/"
			} else {
				destKey = cleanDestPrefix + "/" + relKey
			}
			items = append(items, copyItem{
				srcKey:  item.Key,
				destKey: destKey,
			})
		}

		if res.IsTruncated && res.NextContinuationToken != "" {
			q.Set("continuation-token", res.NextContinuationToken)
		} else {
			break
		}
	}

	// Check if a directory marker without trailing slash exists
	markerReq, err := c.newRequest(ctx, http.MethodHead, srcBucket, cleanSrcPrefix, nil, nil)
	if err == nil {
		if markerResp, err := c.httpClient.Do(markerReq); err == nil {
			_ = markerResp.Body.Close()
			if markerResp.StatusCode == http.StatusOK {
				items = append(items, copyItem{
					srcKey:  cleanSrcPrefix,
					destKey: cleanDestPrefix,
				})
			}
		}
	}

	if len(items) == 0 {
		if err := c.Mkdir(ctx, path.Join(destBucket, cleanDestPrefix)); err != nil {
			return err
		}
		_ = c.DeleteObject(ctx, srcBucket, cleanSrcPrefix+"/")
		_ = c.DeleteObject(ctx, srcBucket, cleanSrcPrefix)
		return nil
	}

	for _, it := range items {
		if err := c.CopyObject(ctx, srcBucket, it.srcKey, destBucket, it.destKey); err != nil {
			return fmt.Errorf("copy %s failed: %w", it.srcKey, err)
		}
		if err := c.DeleteObject(ctx, srcBucket, it.srcKey); err != nil {
			return fmt.Errorf("delete %s failed: %w", it.srcKey, err)
		}
	}

	_ = c.DeleteObject(ctx, srcBucket, cleanSrcPrefix+"/")
	_ = c.DeleteObject(ctx, srcBucket, cleanSrcPrefix)

	return nil
}

// Close closes the S3 client session.
func (c *Client) Close() error {
	if c.httpClient != nil {
		c.httpClient.CloseIdleConnections()
	}
	return nil
}
