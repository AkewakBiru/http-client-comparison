package main

import (
	"crypto/tls"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/valyala/fasthttp"
)

type result struct {
	Url  string `json:"url"`
	Resp string `json:"response"`
	Err  string `json:"error"`
}

var (
	outfile    string
	infile     string
	threads    int = 20
	client     *http.Client
	fastclient fasthttp.Client
)

func main() {
	if len(os.Args) < 2 {
		outfile = "result.json"
		infile = "../urls.json"
	} else if len(os.Args) == 2 {
		infile = os.Args[1]
	} else {
		infile = os.Args[1]
		outfile = os.Args[2]
		if len(os.Args) >= 4 {
			val, err := strconv.Atoi(os.Args[3])
			if err != nil {
				threads = val
			}
		}
	}

	client = &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Timeout: 2 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
	}
	// fasthttp
	fastclient = fasthttp.Client{
		TLSConfig:     &tls.Config{InsecureSkipVerify: true},
		ReadTimeout:   time.Second * 2,
		WriteTimeout:  time.Second * 2,
		DialDualStack: true,
		Dial: func(addr string) (net.Conn, error) {
			return fasthttp.DialDualStackTimeout(addr, 2*time.Second)
		},
	}

	file, err := os.Open(infile)
	if err != nil {
		panic(err)
	}
	defer file.Close()
	dec := json.NewDecoder(file)
	var input []struct {
		Name string `json:"name"`
		Url  string `json:"url"`
	}
	if err := dec.Decode(&input); err != nil {
		panic(err)
	}

	var final []result
	var fastfinal []result
	wg := sync.WaitGroup{}
	ch := make(chan string)
	mu := sync.Mutex{}

	go func() {
		for _, u := range input {
			ch <- u.Url
		}
		close(ch)
	}()

	for range threads {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for in := range ch {
				rsp := send(in)
				fastrsp := fasthttpSend(in)
				mu.Lock()
				final = append(final, rsp)
				fastfinal = append(fastfinal, fastrsp)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	var store map[string][]result = make(map[string][]result)
	store["net/url"] = final
	store["fasthttp"] = fastfinal

	outfile, err := os.OpenFile(outfile, os.O_CREATE|os.O_TRUNC|os.O_RDWR, 0644)
	if err != nil {
		panic(err)
	}
	defer outfile.Close()
	enc := json.NewEncoder(outfile)
	enc.SetIndent("", "  ")
	enc.Encode(store)
}

func send(url string) (res result) {
	res.Url = url
	resp, err := client.Get(url)
	if err != nil {
		res.Url = url
		res.Err = err.Error()
		return res
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		res.Err = err.Error()
		return res
	}
	res.Resp = string(body)
	return res
}

func fasthttpSend(url string) (res result) {
	res.Url = url
	req := fasthttp.AcquireRequest()
	req.SetRequestURI(url)
	req.Header.SetMethod(fasthttp.MethodGet)
	resp := fasthttp.AcquireResponse()
	err := fastclient.Do(req, resp)
	fasthttp.ReleaseRequest(req)
	if err == nil {
		res.Resp = string(resp.Body())
	} else {
		res.Err = err.Error()
	}
	fasthttp.ReleaseResponse(resp)
	return res
}
