package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// any difference between any of the 3 (url parser, dns checker and url requester) could lead to a bypass
var langs = []string{"go", "java", "node", "php", "python", "ruby", "rust", "dotnet/httpclient", "curl", "wget"}

type UrlPart struct {
	Url   string `json:"url"`
	Resp  string `json:"response,omitempty"`
	Error string `json:"error,omitempty"`
}

func main() {
	http.HandleFunc("/refresh", refreshHandler)
	http.HandleFunc("/compare", searchHandler)
	http.HandleFunc("/", indexHandler)

	port := 8080
	log.Printf("\u0085Server started Listenting on port :%d", port)
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", port), nil))
}

func indexHandler(w http.ResponseWriter, r *http.Request) {
	// fmt.Printf("URL:%#v\n", r.URL)
	// fmt.Println(r.Host)
	// fmt.Println(r.URL.String())
	// fmt.Println(r.Header.Get("a"))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, `<!doctype html>
<html data-bs-theme="dark">
<head>
<meta charset="utf-8">
<title>HTTP Client Comparison</title>
<link href="https://cdn.jsdelivr.net/npm/bootstrap@5.3.2/dist/css/bootstrap.min.css" rel="stylesheet">
</head>
<style>
.table-container {
  max-height: 98vh; /* Set a fixed height to enable vertical scrolling */
  overflow-y: auto; /* Enable vertical scrollbar */
  width: 100%;
}

.table-container thead th {
  position: sticky; /* Make the header sticky */
  top: 0; /* Stick it to the top of the container */
  z-index: 1; /* Ensure the header is above other table content */
}

td.resp {
  min-width: 500px;
}

</style>
<body class="text-dark-emphasis">

<div class="container-fluid mt-5">
<h2>Http Client Comparison</h2>

<div class="row g-2">
    <div class="col-12 col-md-9">
        <input id="url" type="text" class="form-control form-control-dark" placeholder="https://example.com">
    </div>
    <div class="col-12 col-md-3">
        <button class="btn btn-secondary w-100" onclick="search()">Compare</button>
    </div>
</div>

<div id="results"></div>
</div>

<script>
async function search() {
    const url = document.getElementById("url").value;
    if (!url) return alert("Enter a URL");

    const res = await fetch("/compare", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ url })
    });

    const html = await res.text();
    document.getElementById("results").innerHTML = html;
}
</script>

</body>
</html>`)
}

// maybe should be a post request
func searchHandler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Url string `json:"url"`
	}
	url := r.URL.Query().Get("url")
	if len(url) == 0 {
		cl := r.Header.Get("Content-Length")
		bodySize := func() int {
			if len(cl) == 0 {
				return 0
			}
			size, err := strconv.Atoi(cl)
			if err != nil {
				return -1
			}
			return size
		}()
		if r.Method != http.MethodPost || bodySize <= 0 {
			return
		}
		dec := json.NewDecoder(r.Body)
		if err := dec.Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		url = body.Url
	}
	res, err := getJSON(url)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	renderHTML(w, url, res)
}

func refreshHandler(w http.ResponseWriter, r *http.Request) {
	_ = w
	_ = r
	fname := r.URL.Query().Get("file")
	if len(fname) == 0 {
		fname = "result.json"
	}
	dir, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	outfile, err := os.OpenFile("aggregated.json", os.O_RDWR|os.O_TRUNC|os.O_CREATE, 0644)
	if err != nil {
		panic(err)
	}
	defer outfile.Close()
	for _, v := range langs {
		infile, err := os.Open(fmt.Sprintf("%s/%s/%s", dir, v, fname))
		if err != nil {
			log.Print(err.Error())
			continue
		}

		var tmp map[string][]UrlPart
		dec := json.NewDecoder(infile)
		if err := dec.Decode(&tmp); err != nil {
			infile.Close()
			log.Print(err.Error())
			continue
		}

		final := prefixMapKeys(tmp, v)
		enc := json.NewEncoder(outfile)
		if err := enc.Encode(final); err != nil {
			infile.Close()
			log.Print(err.Error())
			continue
		}
		infile.Close()
	}
}

func renderHTML(w io.Writer, url string, rows map[string]UrlPart) {
	// Sort frameworks alphabetically
	keys := make([]string, 0, len(rows))
	for k := range rows {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// Start HTML table container
	fmt.Fprintf(w, `<h3 class="mt-3 mb-2">URL: <code class="text-dark-emphasis">%s</code></h3>`, html.EscapeString(url))
	fmt.Fprint(w, `<div class="table-responsive table-container">
<table class="table table-dark table-striped table-hover table-bordered">
<thead>
<tr>
  <th class="text-dark-emphasis">Pkg</th>
  <th class="text-dark-emphasis">Response</th>
  <th class="text-dark-emphasis">Error</th>
</tr>
</thead>
<tbody>`)

	for _, k := range keys {
		p := rows[k]
		fmt.Fprintf(w, `<tr><td class="text-dark-emphasis">%s</td>`, html.EscapeString(k))
		// Helper function to render a cell
		td := func(name, value string) {
			switch name {
			case "error":
				fmt.Fprintf(w, `<td class="text-danger">%s</td>`, html.EscapeString(value))
			case "response":
				fmt.Fprintf(w, `<td class="text-dark-emphasis resp">%s</td>`, value)
			default:
				fmt.Fprintf(w, `<td class="text-dark-emphasis">%s</td>`, html.EscapeString(value))
			}
		}

		// i should
		var final strings.Builder
		tmp := p.Resp
		for {
			idx := strings.Index(tmp, "\r\n")
			if idx == -1 {
				break
			}
			final.WriteString("<code>" + tmp[:idx] + "<span class='text-body-secondary'>\\r\\n</span></code><br>")
			tmp = tmp[idx+2:]
		}

		td("response", final.String())
		td("error", p.Error)
		fmt.Fprint(w, `</tr>`)
	}
	fmt.Fprint(w, `</tbody></table></div>`)
	// Optional: simple custom CSS for error and None styling
	fmt.Fprint(w, `<style>
    td.none { color: #adb5bd; font-style: italic; font-size: 0.85em; }
    </style>`)
}

// a collection of urlpart with a pkg map[string]UrlPart maybe
func getJSON(url string) (map[string]UrlPart, error) {
	if u, err := strconv.Unquote(`"` + url + `"`); err == nil {
		url = u
	}
	fmt.Println(url)
	file, err := os.Open("aggregated.json")
	if err != nil {
		return nil, err
	}
	defer file.Close()

	once := sync.Once{}
	stop := false
	var final map[string]UrlPart = make(map[string]UrlPart)
	rd := bufio.NewReader(file)
	for {
		line, err := rd.ReadBytes(byte('\n'))
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, err
		} else if err != nil && errors.Is(err, io.EOF) {
			break // if EOF, break
		}
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var res map[string][]UrlPart
		dec := json.NewDecoder(bytes.NewBuffer(line))
		if err := dec.Decode(&res); err != nil {
			return nil, err
		}
		for k, vv := range res {
			found := false
			for _, v := range vv {
				if strings.EqualFold(v.Url, url) {
					found = true
					final[k] = v
					break
				}
			}
			once.Do(func() {
				if !found {
					stop = true
				}
			})
			if stop {
				return nil, errors.New("entry not found")
			}
		}
	}
	return final, nil
}

func prefixMapKeys(origMap map[string][]UrlPart, prefix string) map[string][]UrlPart {
	var dst map[string][]UrlPart = make(map[string][]UrlPart)
	for k, v := range origMap {
		dst[fmt.Sprintf("%s/%s", prefix, k)] = v
	}
	clear(origMap)
	return dst
}
