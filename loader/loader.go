package loader

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"sync/atomic"
	"time"

	"github.com/wolviecb/go-werk/util"
)

const (
	userAgent = "go-werk"
)

// ABool is a atomic boolean
type ABool struct {
	flag int32
}

// Set atomically write a bool
func (b *ABool) Set(v bool) {
	var i int32 = 0
	if v {
		i = 1
	}
	atomic.StoreInt32(&(b.flag), int32(i))
}

// Get atomically reads a bool
func (b *ABool) Get() bool {
	if atomic.LoadInt32(&(b.flag)) != 0 {
		return true
	}
	return false
}

// LoadCfg holds configuration data
type LoadCfg struct {
	Duration           int                  // Duration of the test in seconds
	Goroutines         int                  // Number of parallel routines to run
	TestURL            string               // URL to test
	ReqBody            string               // HTTP Request body of the test
	Method             string               // HTTP Method of the test
	Host               string               // Overrides TestURL host
	Header             map[string]string    // HTTP Headers for the test
	StatsAggregator    chan *RequesterStats // Test Results aggregator
	Timeoutms          int                  // HTTP timeout in milliseconds
	AllowRedirects     bool                 // Allow HTTP redirects
	DisableCompression bool                 // Disable HTTP compressions
	DisableKeepAlive   bool                 // Disable HTTP keep-alive
	StopAll            ABool                // Stops all routines
	ClientCert         string               // Client certificate for authentication
	ClientKey          string               // Client key for authentication
	CaCert             string               // CA Certificate
	HTTP2              bool                 // Use HTTP2
	InsecureTLS        bool                 // Toggles remote certificate validation
}

// RequesterStats used for collecting aggregate statistics
type RequesterStats struct {
	TotRespSize    int64
	TotDuration    time.Duration
	MinRequestTime time.Duration
	MaxRequestTime time.Duration
	NumRequests    int
	NumErrs        int
}

// ResponseStats is used to collect HTTP response duration and size
type ResponseStats struct {
	Duration time.Duration
	Size     int64
}

// NewRequest builds a new HTTP request
func NewRequest(method, url, host string, headers map[string]string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		fmt.Println("An error occurred doing request", err)
		return req, err
	}

	for hk, hv := range headers {
		req.Header.Add(hk, hv)
	}

	req.Header.Add("User-Agent", userAgent)
	if host != "" {
		req.Host = host
	}
	return req, nil
}

// NewLoadCfg loads configuration into LoadCfg
func NewLoadCfg(duration int, //seconds
	goroutines int,
	testURL string,
	reqBody string,
	method string,
	host string,
	header map[string]string,
	statsAggregator chan *RequesterStats,
	timeoutms int,
	allowRedirects bool,
	disableCompression bool,
	disableKeepAlive bool,
	clientCert string,
	clientKey string,
	caCert string,
	http2 bool,
	insecureTLS bool) *LoadCfg {
	return &LoadCfg{
		Duration:           duration,
		Goroutines:         goroutines,
		TestURL:            testURL,
		ReqBody:            reqBody,
		Method:             method,
		Host:               host,
		Header:             header,
		StatsAggregator:    statsAggregator,
		Timeoutms:          timeoutms,
		AllowRedirects:     allowRedirects,
		DisableCompression: disableCompression,
		DisableKeepAlive:   disableKeepAlive,
		StopAll:            *new(ABool),
		ClientCert:         clientCert,
		ClientKey:          clientKey,
		CaCert:             caCert,
		HTTP2:              http2,
		InsecureTLS:        insecureTLS,
	}
}

// DoRequest single request implementation. Returns the size of the response and its duration
func (cfg *LoadCfg) DoRequest(httpClient *http.Client) (ResponseStats, error) {
	respStats := ResponseStats{}
	req, err := NewRequest(cfg.Method, cfg.TestURL, cfg.Host, cfg.Header, bytes.NewBufferString(cfg.ReqBody))
	if err != nil {
		fmt.Println("An error occurred doing request", err)
		return respStats, err
	}

	start := time.Now()
	resp, err := httpClient.Do(req)
	if err != nil {
		fmt.Println("redirect?")
		// this is a bit weird. When redirection is prevented, a url.Error is returned. This creates an issue to distinguish
		// between an invalid URL that was provided and and redirection error.
		rr, ok := err.(*url.Error)
		if !ok {
			fmt.Println("An error occurred doing request", err, rr)
			return respStats, err
		}
		fmt.Println("An error occurred doing request", err)
	}
	if resp == nil {
		fmt.Println("empty response")
		return respStats, err
	}
	defer func() {
		if resp != nil && resp.Body != nil {
			resp.Body.Close()
		}
	}()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		fmt.Println("An error occurred reading body", err)
	}
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated {
		respStats.Duration = time.Since(start)
		respStats.Size = int64(len(body)) + util.EstimateHTTPHeadersSize(resp.Header)
	} else if resp.StatusCode == http.StatusMovedPermanently || resp.StatusCode == http.StatusTemporaryRedirect {
		respStats.Duration = time.Since(start)
		respStats.Size = resp.ContentLength + util.EstimateHTTPHeadersSize(resp.Header)
	} else {
		fmt.Println("received status code", resp.StatusCode, "from", resp.Header, "content", string(body), req)
	}

	return respStats, err
}

// RunSingleLoadSession Requester a go function for repeatedly making requests and aggregating statistics as long as required
// When it is done, it sends the results using the statsAggregator channel
func (cfg *LoadCfg) RunSingleLoadSession() {
	stats := &RequesterStats{MinRequestTime: time.Minute}
	start := time.Now()

	httpClient, err := client(*cfg)
	if err != nil {
		log.Fatal(err)
	}

	for time.Since(start).Seconds() <= float64(cfg.Duration) && !cfg.StopAll.Get() {
		respStats, err := cfg.DoRequest(httpClient)
		if err != nil {
			stats.NumErrs++
			continue
		}
		stats.TotRespSize += respStats.Size
		stats.TotDuration += respStats.Duration
		stats.MaxRequestTime = util.MaxDuration(respStats.Duration, stats.MaxRequestTime)
		stats.MinRequestTime = util.MinDuration(respStats.Duration, stats.MinRequestTime)
		stats.NumRequests++

	}
	cfg.StatsAggregator <- stats
}
