---
sidebar_position: 2
---

# Working with Remote Archives

How to access blob archives stored in OCI registries via HTTP range requests.

## Setting Up an HTTP Source

To read an archive from a remote URL, create an HTTP source:

```go
import (
	"io"
	nethttp "net/http"

	"github.com/meigma/blob"
	"github.com/meigma/blob/http"
)

func openRemoteArchive(indexURL, dataURL string) (*blob.Blob, error) {
	// Fetch the index (small, can be fetched entirely)
	resp, err := nethttp.Get(indexURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	indexData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// Create an HTTP source for the data blob
	source, err := http.NewSource(dataURL)
	if err != nil {
		return nil, err
	}

	return blob.New(indexData, source)
}
```

`NewSource` probes the remote server to verify range request support and determine content size. If the server does not support range requests, NewSource returns an error.

## Authentication

To access protected resources, add authentication headers:

```go
// Bearer token authentication
source, err := http.NewSource(dataURL,
	http.WithHeader("Authorization", "Bearer "+token),
)
```

For multiple headers, use `WithHeaders`:

```go
headers := make(nethttp.Header)
headers.Set("Authorization", "Bearer "+token)
headers.Set("X-Custom-Header", "value")

source, err := http.NewSource(dataURL,
	http.WithHeaders(headers),
)
```

### OCI Registry Authentication

For OCI registries, obtain a token and pass it as a bearer token:

```go
func openOCIArchive(registryURL, repo, digest, token string) (*blob.Blob, error) {
	dataURL := fmt.Sprintf("%s/v2/%s/blobs/%s", registryURL, repo, digest)

	source, err := http.NewSource(dataURL,
		http.WithHeader("Authorization", "Bearer "+token),
	)
	if err != nil {
		return nil, err
	}

	// Index would be fetched similarly with the token
	// ...

	return blob.New(indexData, source)
}
```

## Custom HTTP Clients

For advanced configurations like timeouts, retries, or proxies, provide a custom HTTP client:

```go
import (
	nethttp "net/http"
	"time"

	"github.com/meigma/blob/http"
)

// Client with timeout
client := &nethttp.Client{
	Timeout: 30 * time.Second,
}

source, err := http.NewSource(dataURL,
	http.WithClient(client),
)
```

### Retry Configuration

For production use, configure retry logic:

```go
import (
	"github.com/hashicorp/go-retryablehttp"
)

// Create a retrying HTTP client
retryClient := retryablehttp.NewClient()
retryClient.RetryMax = 3
retryClient.RetryWaitMin = 1 * time.Second
retryClient.RetryWaitMax = 10 * time.Second

source, err := http.NewSource(dataURL,
	http.WithClient(retryClient.StandardClient()),
)
```

### Proxy Configuration

To route requests through a proxy:

```go
proxyURL, _ := url.Parse("http://proxy.example.com:8080")
transport := &nethttp.Transport{
	Proxy: nethttp.ProxyURL(proxyURL),
}
client := &nethttp.Client{
	Transport: transport,
}

source, err := http.NewSource(dataURL,
	http.WithClient(client),
)
```

## Error Handling

Handle common error scenarios:

```go
source, err := http.NewSource(dataURL)
if err != nil {
	// Check for range request support
	if strings.Contains(err.Error(), "range requests not supported") {
		return nil, fmt.Errorf("server does not support range requests: %w", err)
	}
	// Network errors, DNS failures, etc.
	return nil, fmt.Errorf("failed to connect: %w", err)
}

archive, err := blob.New(indexData, source)
if err != nil {
	return nil, fmt.Errorf("invalid archive: %w", err)
}

// Reading files may fail with network errors
content, err := archive.ReadFile("path/to/file")
if err != nil {
	// Could be network error, hash mismatch, or file not found
	return nil, fmt.Errorf("read file: %w", err)
}
```

### Checking Range Request Support

The HTTP source validates range request support during creation. If you need to check separately:

```go
func supportsRangeRequests(url string) bool {
	req, _ := nethttp.NewRequest("GET", url, nil)
	req.Header.Set("Range", "bytes=0-0")

	resp, err := nethttp.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == nethttp.StatusPartialContent
}
```

## Complete Example

A complete function for opening a remote archive with authentication and error handling:

```go
func openRemoteArchive(indexURL, dataURL, token string) (*blob.Blob, error) {
	// Configure HTTP client with timeout and retries
	client := &nethttp.Client{
		Timeout: 60 * time.Second,
	}

	// Fetch index data
	req, err := nethttp.NewRequest("GET", indexURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch index: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != nethttp.StatusOK {
		return nil, fmt.Errorf("fetch index: %s", resp.Status)
	}

	indexData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read index: %w", err)
	}

	// Create data source with authentication
	var opts []http.Option
	opts = append(opts, http.WithClient(client))
	if token != "" {
		opts = append(opts, http.WithHeader("Authorization", "Bearer "+token))
	}

	source, err := http.NewSource(dataURL, opts...)
	if err != nil {
		return nil, fmt.Errorf("create source: %w", err)
	}

	archive, err := blob.New(indexData, source)
	if err != nil {
		return nil, fmt.Errorf("open archive: %w", err)
	}

	return archive, nil
}
```

## See Also

- [Caching](caching) - Cache remote content for faster repeated access
- [Block Caching](block-caching) - Block-level caching for random access reads
- [Performance Tuning](performance-tuning) - Optimize for network latency
