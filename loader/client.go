package loader

import (
	"crypto/tls"
	"crypto/x509"
	"io/ioutil"
	"net/http"

	"fmt"

	"time"

	"github.com/wolviecb/go-werk/util"
	"golang.org/x/net/http2"
)

func client(cfg LoadCfg) (*http.Client, error) {

	c := &http.Client{}
	// overriding the defaults with cfg parameters
	c.Transport = &http.Transport{
		DisableCompression:    cfg.DisableCompression,
		DisableKeepAlives:     cfg.DisableKeepAlive,
		ResponseHeaderTimeout: time.Millisecond * time.Duration(cfg.Timeoutms),
	}

	if !cfg.AllowRedirects {
		// returning an error when trying to redirect. This prevents the redirection from happening.
		c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			return util.NewRedirectError("redirection not allowed")
		}
	}

	if cfg.ClientCert != "" && cfg.ClientKey != "" && cfg.CaCert != "" {
		cert, err := tls.LoadX509KeyPair(cfg.ClientCert, cfg.ClientKey)
		if err != nil {
			return nil, fmt.Errorf("Unable to load cert tried to load %v and %v but got %v", cfg.ClientCert, cfg.ClientKey, err)
		}

		// Load our CA certificate
		clientCACert, err := ioutil.ReadFile(cfg.CaCert)
		if err != nil {
			return nil, fmt.Errorf("Unable to open cert %v", err)
		}

		clientCertPool := x509.NewCertPool()
		clientCertPool.AppendCertsFromPEM(clientCACert)

		tlsConfig := &tls.Config{
			Certificates:       []tls.Certificate{cert},
			RootCAs:            clientCertPool,
			InsecureSkipVerify: cfg.InsecureTLS,
		}

		tlsConfig.BuildNameToCertificate()
		t := &http.Transport{
			TLSClientConfig: tlsConfig,
		}
		if cfg.HTTP2 {
			http2.ConfigureTransport(t)
		}
		c.Transport = t
		return c, nil
	}

	t := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: cfg.InsecureTLS,
		},
	}

	if cfg.HTTP2 {
		http2.ConfigureTransport(t)
	}
	c.Transport = t
	return c, nil
}
