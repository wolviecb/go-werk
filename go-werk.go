package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"time"

	"github.com/wolviecb/go-werk/loader"
	"github.com/wolviecb/go-werk/util"
)

const appVersion = "0.1"

// default that can be overridden from the command line
var versionFlag bool
var allowRedirectsFlag bool
var helpFlag bool
var disableCompression bool
var disableKeepAlive bool
var goroutines int
var duration int
var timeoutms int
var method string
var host string
var headerStr string
var reqBody string
var clientCert string
var clientKey string
var caCert string
var http2 bool
var insecureTLS bool
var testURL string
var header map[string]string
var statsAggregator chan *loader.RequesterStats

func init() {
	flag.BoolVar(&versionFlag, "v", false, "Print version details")
	flag.BoolVar(&allowRedirectsFlag, "redir", false, "Allow Redirects")
	flag.BoolVar(&helpFlag, "help", false, "Print help")
	flag.BoolVar(&disableCompression, "no-c", false, "Disable Compression - Prevents sending the \"Accept-Encoding: gzip\" header")
	flag.BoolVar(&disableKeepAlive, "no-ka", false, "Disable KeepAlive - prevents re-use of TCP connections between different HTTP requests")
	flag.IntVar(&goroutines, "c", 10, "Number of goroutines to use (concurrent connections)")
	flag.IntVar(&duration, "d", 10, "Duration of test in seconds")
	flag.IntVar(&timeoutms, "T", 1000, "Socket/request timeout in ms")
	flag.StringVar(&method, "M", "GET", "HTTP method")
	flag.StringVar(&host, "host", "", "Host Header")
	flag.StringVar(&headerStr, "H", "", "header line, joined with ';'")
	flag.StringVar(&reqBody, "body", "", "request body string or @filename")
	flag.StringVar(&clientCert, "cert", "", "CA certificate file to verify peer against (SSL/TLS)")
	flag.StringVar(&clientKey, "key", "", "Private key file name (SSL/TLS")
	flag.StringVar(&caCert, "ca", "", "CA file to verify peer against (SSL/TLS)")
	flag.BoolVar(&http2, "http", true, "Use HTTP/2")
	flag.BoolVar(&insecureTLS, "insecure", true, "verify TLS certificates")
}

// printDefaults a nicer format for the defaults
func printDefaults() {
	fmt.Println("Usage: go-werk <options> <url>")
	fmt.Println("Options:")
	flag.VisitAll(func(flag *flag.Flag) {
		fmt.Println("\t-"+flag.Name, "\t", flag.Usage, "(Default "+flag.DefValue+")")
	})
}

func main() {
	// raising the limits. Some performance gains were achieved with the + goroutines (not a lot).
	runtime.GOMAXPROCS(runtime.NumCPU() + goroutines)

	statsAggregator = make(chan *loader.RequesterStats, goroutines)
	sigChan := make(chan os.Signal, 1)

	signal.Notify(sigChan, os.Interrupt)

	flag.Parse() // Scan the arguments list
	header = make(map[string]string)
	if headerStr != "" {
		headerPairs := strings.Split(headerStr, ";")
		for _, hdr := range headerPairs {
			hp := strings.SplitN(hdr, ":", 2)
			header[hp[0]] = hp[1]
		}
	}

	if na := flag.Args(); len(na) < 1 {
		printDefaults()
	} else {
		testURL = flag.Args()[0]
	}

	if versionFlag {
		fmt.Println("Version:", appVersion)
		return
	} else if helpFlag || len(testURL) == 0 {
		printDefaults()
		return
	}

	fmt.Printf("Running %vs test @ %v\n  %v goroutine(s) running concurrently\n", duration, testURL, goroutines)

	if len(reqBody) > 0 && reqBody[0] == '@' {
		bodyFilename := reqBody[1:]
		data, err := ioutil.ReadFile(bodyFilename)
		if err != nil {
			fmt.Println(fmt.Errorf("could not read file %q: %v", bodyFilename, err))
			os.Exit(1)
		}
		reqBody = string(data)
	}

	loadGen := loader.NewLoadCfg(duration, goroutines, testURL, reqBody, method, host, header, statsAggregator, timeoutms,
		allowRedirectsFlag, disableCompression, disableKeepAlive, clientCert, clientKey, caCert, http2, insecureTLS)

	for i := 0; i < goroutines; i++ {
		go loadGen.RunSingleLoadSession()
	}

	responders := 0
	aggStats := loader.RequesterStats{MinRequestTime: time.Minute}

	for responders < goroutines {
		select {
		case <-sigChan:
			loadGen.Stop()
			fmt.Printf("stopping...\n")
		case stats := <-statsAggregator:
			aggStats.NumErrs += stats.NumErrs
			aggStats.NumRequests += stats.NumRequests
			aggStats.TotRespSize += stats.TotRespSize
			aggStats.TotDuration += stats.TotDuration
			aggStats.MaxRequestTime = util.MaxDuration(aggStats.MaxRequestTime, stats.MaxRequestTime)
			aggStats.MinRequestTime = util.MinDuration(aggStats.MinRequestTime, stats.MinRequestTime)
			responders++
		}
	}

	if aggStats.NumRequests == 0 {
		fmt.Println("Error: No statistics collected / no requests found")
		return
	}

	avgThreadDur := aggStats.TotDuration / time.Duration(responders) //need to average the aggregated duration

	reqRate := float64(aggStats.NumRequests) / avgThreadDur.Seconds()
	avgReqTime := aggStats.TotDuration / time.Duration(aggStats.NumRequests)
	bytesRate := float64(aggStats.TotRespSize) / avgThreadDur.Seconds()
	fmt.Printf("%v requests in %v, %v read\n", aggStats.NumRequests, avgThreadDur, util.ByteSize{Size: float64(aggStats.TotRespSize)})
	fmt.Printf("Requests/sec:\t\t%.2f\nTransfer/sec:\t\t%v\nAvg Req Time:\t\t%v\n", reqRate, util.ByteSize{Size: bytesRate}, avgReqTime)
	fmt.Printf("Fastest Request:\t%v\n", aggStats.MinRequestTime)
	fmt.Printf("Slowest Request:\t%v\n", aggStats.MaxRequestTime)
	fmt.Printf("Number of Errors:\t%v\n", aggStats.NumErrs)

}
