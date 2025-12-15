package commands

import (
	"fmt"
	"math"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"runtime"
	"time"

	"miren.dev/runtime/pkg/hey/requester"
)

// this is a port of of https://github.com/rakyll/hey, made available here for
// simplicity and usability by runtime users.

const (
	headerRegexp = `^([\w-]+):\s*(.+)`
	authRegexp   = `^(.+):([^\s].+)`
	heyUA        = "miren-hey/0.0.2"
)

func parseInputWithRegexp(input, regx string) ([]string, error) {
	re := regexp.MustCompile(regx)
	matches := re.FindStringSubmatch(input)
	if len(matches) < 1 {
		return nil, fmt.Errorf("could not parse the provided input; input = %v", input)
	}
	return matches, nil
}

func TestLoad(ctx *Context, opts struct {
	Requests    int           `short:"n" long:"requests" description:"Number of requests to make" default:"200"`
	Concurrency int           `short:"c" long:"concurrency" description:"Number of concurrent requests to make" default:"50"`
	RateLimit   float64       `short:"q" long:"rate-limit" description:"Rate limit in requests per second" default:"0"`
	Duration    time.Duration `short:"z" long:"duration" description:"Duration of the test"`

	Method      string   `short:"m" long:"method" description:"HTTP method to use" default:"GET"`
	Header      []string `short:"H" long:"header" description:"HTTP header to use"`
	Timeout     int      `short:"t" long:"timeout" description:"Timeout for each request in seconds" default:"20"`
	HTTP2       bool     `long:"h2" description:"Use HTTP/2"`
	Host        string   `long:"host" description:"Host header to use"`
	ContentType string   `short:"T" long:"content-type" description:"Content-Type header to use" default:"text/html"`
	AuthHeader  string   `short:"a" long:"auth" description:"Basic auth header to use"`
	Accept      string   `short:"A" long:"accept" description:"Accept header to use"`
	Body        string   `short:"d" long:"data" description:"HTTP request body"`
	BodyFile    string   `short:"D" long:"data-file" description:"File to use as request body"`
	ProxyAddr   string   `short:"x" long:"proxy" description:"Proxy URL to use"`
	UserAgent   string   `short:"U" long:"user-agent" description:"User-Agent header to use"`

	CPUS   *int   `long:"cpus" description:"Number of CPUs to use"`
	Output string `short:"o" long:"output" description:"Output type, the only support value is 'csv'"`

	DisableCompression bool `long:"disable-compression" description:"Disable compression"`
	DisableKeepAlives  bool `long:"disable-keepalives" description:"Disable keep-alives"`
	DisableRedirects   bool `long:"disable-redirects" description:"Disable redirects"`

	URL string `position:"0" usage:"URL to load test" required:"true"`
}) error {
	if opts.CPUS != nil {
		runtime.GOMAXPROCS(*opts.CPUS)
	}

	if opts.Concurrency <= 0 {
		return fmt.Errorf("concurrency must be greater than 0")
	}

	if opts.Duration > 0 {
		opts.Requests = math.MaxInt
	} else {
		if opts.Requests < opts.Concurrency {
			return fmt.Errorf("number of requests must be greater than or equal to concurrency")
		}
	}

	header := make(http.Header)
	header.Set("Content-Type", opts.ContentType)

	var host string

	// set any other additional repeatable headers
	for _, h := range opts.Header {
		match, err := parseInputWithRegexp(h, headerRegexp)
		if err != nil {
			return fmt.Errorf("could not parse the provided header %q", h)
		}

		if match[1] == "Host" {
			host = match[2]
		} else {
			header.Set(match[1], match[2])
		}
	}

	if opts.Accept != "" {
		header.Set("Accept", opts.Accept)
	}

	// set basic auth if set
	var username, password string
	if opts.AuthHeader != "" {
		match, err := parseInputWithRegexp(opts.AuthHeader, authRegexp)
		if err != nil {
			return fmt.Errorf("could not parse the provided auth header %q", opts.AuthHeader)
		}
		username, password = match[1], match[2]
	}

	var bodyAll []byte
	if opts.Body != "" {
		bodyAll = []byte(opts.Body)
	}
	if opts.BodyFile != "" {
		slurp, err := os.ReadFile(opts.BodyFile)
		if err != nil {
			return fmt.Errorf("could not read the provided body file %q: %w", opts.BodyFile, err)
		}
		bodyAll = slurp
	}

	var proxyURL *url.URL
	if opts.ProxyAddr != "" {
		var err error
		proxyURL, err = url.Parse(opts.ProxyAddr)
		if err != nil {
			return fmt.Errorf("could not parse the provided proxy URL %q: %w", opts.ProxyAddr, err)
		}
	}

	req, err := http.NewRequest(opts.Method, opts.URL, nil)
	if err != nil {
		return fmt.Errorf("could not create the request: %w", err)
	}
	req.ContentLength = int64(len(bodyAll))
	if username != "" || password != "" {
		req.SetBasicAuth(username, password)
	}

	// set host header if set
	if opts.Host != "" {
		req.Host = opts.Host
	}

	ua := header.Get("User-Agent")
	if ua == "" {
		ua = heyUA
	} else {
		ua += " " + heyUA
	}
	header.Set("User-Agent", ua)

	// set userAgent header if set
	if opts.UserAgent != "" {
		ua = opts.UserAgent + " " + heyUA
		header.Set("User-Agent", ua)
	}

	if host != "" {
		req.Host = host
	}

	req.Header = header

	w := &requester.Work{
		Request:            req,
		RequestBody:        bodyAll,
		N:                  opts.Requests,
		C:                  opts.Concurrency,
		QPS:                opts.RateLimit,
		Timeout:            opts.Timeout,
		DisableCompression: opts.DisableCompression,
		DisableKeepAlives:  opts.DisableKeepAlives,
		DisableRedirects:   opts.DisableRedirects,
		H2:                 opts.HTTP2,
		ProxyAddr:          proxyURL,
		Output:             opts.Output,
	}
	w.Init()

	go func() {
		<-ctx.Done()
		w.Stop()
	}()

	if opts.Duration > 0 {
		go func() {
			time.Sleep(opts.Duration)
			w.Stop()
		}()
	}
	w.Run()

	return nil
}
