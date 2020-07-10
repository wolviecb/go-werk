package loader

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/wolviecb/go-werk/util"
)

const (
	userAgent = "go-werk"
)

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
	TestURL string,
	ReqBody string,
	Method string,
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
	insecureTLS bool) (rt *LoadCfg) {
	rt = &LoadCfg{duration, goroutines, TestURL, ReqBody, Method, host, header, statsAggregator, timeoutms,
		allowRedirects, disableCompression, disableKeepAlive, 0, clientCert, clientKey, caCert, http2, insecureTLS}
	return
}

func escapeURLStr(in string) string {
	qm := strings.Index(in, "?")
	if qm != -1 {
		qry := in[qm+1:]
		qrys := strings.Split(qry, "&")
		var query string
		var qEscaped string
		var first bool = true
		for _, q := range qrys {
			qSplit := strings.Split(q, "=")
			if len(qSplit) == 2 {
				qEscaped = qSplit[0] + "=" + url.QueryEscape(qSplit[1])
			} else {
				qEscaped = qSplit[0]
			}
			if first {
				first = false
			} else {
				query += "&"
			}
			query += qEscaped

		}
		return in[:qm] + "?" + query
	}
	return in

}

// DoRequest single request implementation. Returns the size of the response and its duration
// On error - returns -1 on both
func (cfg *LoadCfg) DoRequest(httpClient *http.Client) (respSize int, duration time.Duration) {
	respSize = -1
	duration = -1

	loadURL := escapeURLStr(cfg.TestURL)

	var buf io.Reader
	if len(cfg.ReqBody) > 0 {
		buf = bytes.NewBufferString(cfg.ReqBody)
	}

	req, err := http.NewRequest(cfg.Method, loadURL, buf)
	if err != nil {
		fmt.Println("An error occurred doing request", err)
		return
	}

	for hk, hv := range cfg.Header {
		req.Header.Add(hk, hv)
	}

	req.Header.Add("User-Agent", userAgent)
	if cfg.Host != "" {
		req.Host = cfg.Host
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
			return
		}
		fmt.Println("An error occurred doing request", err)
	}
	if resp == nil {
		fmt.Println("empty response")
		return
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
		duration = time.Since(start)
		respSize = len(body) + int(util.EstimateHTTPHeadersSize(resp.Header))
	} else if resp.StatusCode == http.StatusMovedPermanently || resp.StatusCode == http.StatusTemporaryRedirect {
		duration = time.Since(start)
		respSize = int(resp.ContentLength) + int(util.EstimateHTTPHeadersSize(resp.Header))
	} else {
		fmt.Println("received status code", resp.StatusCode, "from", resp.Header, "content", string(body), req)
	}

	return
}

// RunSingleLoadSession Requester a go function for repeatedly making requests and aggregating statistics as long as required
// When it is done, it sends the results using the statsAggregator channel
func (cfg *LoadCfg) RunSingleLoadSession() {
	stats := &RequesterStats{MinRequestTime: time.Minute}
	start := time.Now()

	httpClient, err := client(cfg.DisableCompression, cfg.DisableKeepAlive, cfg.Timeoutms, cfg.AllowRedirects, cfg.ClientCert, cfg.ClientKey, cfg.CaCert, cfg.HTTP2, cfg.InsecureTLS)
	if err != nil {
		log.Fatal(err)
	}

	for time.Since(start).Seconds() <= float64(cfg.Duration) && atomic.LoadInt32(&cfg.Interrupted) == 0 {
		respSize, reqDur := cfg.DoRequest(httpClient)
		if respSize > 0 {
			stats.TotRespSize += int64(respSize)
			stats.TotDuration += reqDur
			stats.MaxRequestTime = util.MaxDuration(reqDur, stats.MaxRequestTime)
			stats.MinRequestTime = util.MinDuration(reqDur, stats.MinRequestTime)
			stats.NumRequests++
		} else {
			stats.NumErrs++
		}
	}
	cfg.StatsAggregator <- stats
}

// Stop kill all goroutines
func (cfg *LoadCfg) Stop() {
	atomic.StoreInt32(&cfg.Interrupted, 1)
}
