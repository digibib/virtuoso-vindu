package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/golang/gddo/httputil"
	"github.com/knakk/kbp/rdf"
)

const (
	descQuery  = `DEFINE sql:describe-mode "CBD" DESCRIBE <%s%s>`
	htmlHeader = `<html><head><title>%s</title></head><body><pre>@base              &lt;http://data.deichman.no.no/&gt .
@prefix     deich: &lt;http://data.deichman.no/ontology#&gt; .
@prefix       raw: &lt;http://data.deichman.no/raw#&gt; .
@prefix migration: &lt;http://migration.deichman.no/&gt; .

`
	htmlFooter = `</pre></body></html>`
)

var repl = strings.NewReplacer(
	"http://data.deichman.no/ontology#", "deich:",
	"http://www.w3.org/1999/02/22-rdf-syntax-ns#type", "a",
	"http://data.deichman.no/raw#", "raw:",
	"http://migration.deichman.no/", "migration:",
)

var rgxpLinkify = regexp.MustCompile(`http://data.deichman.no/(place|publication|work|person|corporation|subject|genre|serial)/`)

type server struct {
	graph  string
	base   string
	target string
}

func (srv server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/favicon.ico" {
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return
	}

	format := httputil.NegotiateContentType(r, []string{"text/plain", "text/turtle", "application/rdf+xml", "text/html"}, "text/plain")
	accept := format
	if accept == "text/html" {
		accept = "text/plain"
	}
	log.Println(r.URL.Path)
	params := url.Values{}
	params.Set("query", fmt.Sprintf(descQuery, srv.base, r.URL.Path))
	params.Set("default-graph-uri", srv.graph)
	params.Set("format", accept)
	params.Encode()

	req, err := http.NewRequest("POST", srv.target+params.Encode(), nil)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	if format != "text/html" {
		if _, err := io.Copy(w, resp.Body); err != nil {
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		}
		return
	}

	var trs []rdf.Triple
	dec := rdf.NewDecoder(resp.Body)
	for tr, err := dec.Decode(); err != io.EOF; tr, err = dec.Decode() {
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		trs = append(trs, tr)
	}

	if len(trs) == 0 {
		http.NotFound(w, r)
		return
	}

	sort.Slice(trs, func(i, j int) bool {
		// Sort by subject, then by predicate
		switch strings.Compare(trs[i].Subject.String(), trs[j].Subject.String()) {
		case -1:
			return true
		case 1:
			return false
		}
		return repl.Replace(trs[i].Predicate.Name()) < repl.Replace(trs[j].Predicate.Name())
	})

	node := rdf.NewNamedNode(srv.base + r.URL.Path)
	w.Header().Add("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, htmlHeader, node)

	fmt.Fprintf(w, "<strong>&lt;%s&gt</strong>\n", r.URL.Path[1:])
	tw := tabwriter.NewWriter(w, 0, 0, 4, ' ', 0)
	srv.describe(tw, trs, node)
	tw.Flush()
	w.Write([]byte(" .\n"))
	w.Write([]byte(htmlFooter))
}

func (srv server) describe(w io.Writer, trs []rdf.Triple, node rdf.Node) {
	var curPred rdf.NamedNode
	first := true
	_, inBlank := node.(rdf.BlankNode)
	indent := "\t"
	if inBlank {
		indent = "\t  "
	}
	for _, tr := range trs {
		if node != tr.Subject {
			continue
		}
		if curPred != tr.Predicate {
			curPred = tr.Predicate
			if first {
				fmt.Fprintf(w, "%s%v\t", indent, repl.Replace(tr.Predicate.Name()))
				first = false
			} else {
				fmt.Fprintf(w, " ;\n%s%v\t", indent, repl.Replace(tr.Predicate.Name()))
			}
		} else {
			// object list
			fmt.Fprintf(w, ",\n\t\t")
		}
		switch obj := tr.Object.(type) {
		case rdf.NamedNode:
			if rgxpLinkify.MatchString(obj.Name()) {
				fmt.Fprintf(w, `<a href="/%[1]s">&lt;%[1]s&gt</a>`, strings.TrimPrefix(obj.Name(), srv.base+"/"))
			} else {
				fmt.Fprintf(w, `&lt;%s&gt;`, obj.Name())
			}
		case rdf.BlankNode:
			fmt.Fprintf(w, "[\n")
			srv.describe(w, trs, tr.Object)
			fmt.Fprintf(w, "\n\t]")
		case rdf.Literal:
			fmt.Fprintf(w, "%q", obj.ValueAsString())
		}
	}
}

func main() {
	var (
		graph          = flag.String("graph", "lsext", "Graph to expose")
		sparqlEndpoint = flag.String("sparq", "http://virtuoso:8890/sparql/", "SPARQL endpoint address")
	)
	flag.Parse()

	srv := server{
		graph:  *graph,
		target: *sparqlEndpoint + "?",
		base:   "http://data.deichman.no",
	}

	if err := http.ListenAndServe(":7777", srv); err != nil {
		log.Fatal(err)
	}
}
