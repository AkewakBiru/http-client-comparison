package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"time"
)

type result struct {
	Url  string `json:"url"`
	Resp string `json:"response"`
	Err  string `json:"error"`
}

var (
	outfile string
	infile  string
	threads int = 20
	client  *http.Client
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
				mu.Lock()
				final = append(final, rsp)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	var store map[string][]result = make(map[string][]result)
	store["wget"] = final

	outfile, err := os.OpenFile(outfile, os.O_CREATE|os.O_TRUNC|os.O_RDWR, 0644)
	if err != nil {
		panic(err)
	}
	defer outfile.Close()
	enc := json.NewEncoder(outfile)
	enc.SetIndent("", "  ")
	enc.Encode(store)
}

// wget --timeout=2 --no-check-certificate --max-redirect=0
func send(url string) (res result) {
	res.Url = url
	cmd := exec.Command(
		"wget",
		"--timeout", "2",
		"--no-check-certificate",
		"-O", "-",
		"--no-verbose",
		"--tries", "1",
		"--max-redirect", "0",
		url,
	)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	out := stdout.String()
	if err != nil && len(out) == 0 {
		res.Url = url
		res.Err = stderr.String()
		if len(res.Err) == 0 {
			res.Err = fmt.Sprintf("%s", err.Error())
		}
		return res
	}
	if tmp := stderr.String(); tmp != "" && len(out) == 0 {
		res.Url = url
		res.Err = tmp
		return res
	}
	res.Resp = out
	return res
}
