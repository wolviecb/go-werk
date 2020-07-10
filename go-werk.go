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

// printDefaults a nicer format for the defaults
func printDefaults() {
	fmt.Println("Usage: go-werk <options> <url>")
	fmt.Println("Options:")
	flag.VisitAll(func(flag *flag.Flag) {
		fmt.Println("\t-"+flag.Name, "\t", flag.Usage, "(Default "+flag.DefValue+")")
	})
	os.Exit(0)
}

func main() {
	helpFlag := false
	headerStr := ""
	versionFlag := false
	f := loader.LoadCfg{}
	flag.BoolVar(&versionFlag, "v", false, "Print version details")
	flag.BoolVar(&f.AllowRedirects, "redir", false, "Allow Redirects")
	flag.BoolVar(&helpFlag, "help", false, "Print help")
	flag.BoolVar(&f.DisableCompression, "no-c", false, "Disable Compression - Prevents sending the \"Accept-Encoding: gzip\" header")
	flag.BoolVar(&f.DisableKeepAlive, "no-ka", false, "Disable KeepAlive - prevents re-use of TCP connections between different HTTP requests")
	flag.IntVar(&f.Goroutines, "c", 10, "Number of goroutines to use (concurrent connections)")
	flag.IntVar(&f.Duration, "d", 10, "Duration of test in seconds")
	flag.IntVar(&f.Timeoutms, "T", 1000, "Socket/request timeout in ms")
	flag.StringVar(&f.Method, "M", "GET", "HTTP method")
	flag.StringVar(&f.Host, "host", "", "Host Header")
	flag.StringVar(&headerStr, "H", "", "header line, joined with ';'")
	flag.StringVar(&f.ReqBody, "body", "", "request body string or @filename")
	flag.StringVar(&f.ClientCert, "cert", "", "CA certificate file to verify peer against (SSL/TLS)")
	flag.StringVar(&f.ClientKey, "key", "", "Private key file name (SSL/TLS")
	flag.StringVar(&f.CaCert, "ca", "", "CA file to verify peer against (SSL/TLS)")
	flag.BoolVar(&f.HTTP2, "http", true, "Use HTTP/2")
	flag.BoolVar(&f.InsecureTLS, "insecure", true, "toggle verify TLS certificates")
	flag.Parse() // Scan the arguments list

	// raising the limits. Some performance gains were achieved with the + goroutines (not a lot).
	runtime.GOMAXPROCS(runtime.NumCPU() + f.Goroutines)

	f.StatsAggregator = make(chan *loader.RequesterStats, f.Goroutines)
	sigChan := make(chan os.Signal, 1)

	signal.Notify(sigChan, os.Interrupt)
	f.Header = make(map[string]string)
	if headerStr != "" {
		headerPairs := strings.Split(headerStr, ";")
		for _, hdr := range headerPairs {
			hp := strings.SplitN(hdr, ":", 2)
			f.Header[hp[0]] = hp[1]
		}
	}

	if len(flag.Args()) < 1 {
		printDefaults()
	} else {
		f.TestURL = flag.Args()[0]
	}

	if versionFlag {
		fmt.Println("Version:", appVersion)
		return
	} else if helpFlag || len(f.TestURL) == 0 {
		printDefaults()
	}

	fmt.Printf("Running %vs test @ %v\n  %v goroutine(s) running concurrently\n", f.Duration, f.TestURL, f.Goroutines)

	if len(f.ReqBody) > 0 && f.ReqBody[0] == '@' {
		bodyFilename := f.ReqBody[1:]
		data, err := ioutil.ReadFile(bodyFilename)
		if err != nil {
			fmt.Println(fmt.Errorf("could not read file %q: %v", bodyFilename, err))
			os.Exit(1)
		}
		f.ReqBody = string(data)
	}

	for i := 0; i < f.Goroutines; i++ {
		go f.RunSingleLoadSession()
	}

	responders := 0
	aggStats := loader.RequesterStats{MinRequestTime: time.Minute}

	for responders < f.Goroutines {
		select {
		case <-sigChan:
			f.Stop()
			fmt.Printf("stopping...\n")
		case stats := <-f.StatsAggregator:
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
