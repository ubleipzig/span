// span-tag takes an intermediate schema file and a configuration forest of
// filters for various tags and runs all filters on every record of the input
// to produce a stream of tagged records.
//
// TODO(miku): Allow to skip label attachment by inspecting a SOLR index on the
// fly. Calculate label attachments for record, query index for doi or similar
// id, if the preferred source is already in the index, drop the label. If the
// unpreferred source is indexed, we cannot currently update the index, so just
// emit a warning and do not change anything.
//
// $ span-tag -c '{"DE-15": {"any": {}}}' < input.ldj > output.ldj
//
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"runtime/pprof"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/miku/span"
	"github.com/miku/span/filter"
	"github.com/miku/span/formats/finc"
	"github.com/miku/span/parallel"
	"github.com/miku/span/solrutil"
)

var (
	config               = flag.String("c", "", "JSON config file for filters")
	version              = flag.Bool("v", false, "show version")
	size                 = flag.Int("b", 20000, "batch size")
	numWorkers           = flag.Int("w", runtime.NumCPU(), "number of workers")
	cpuProfile           = flag.String("cpuprofile", "", "write cpu profile to file")
	unfreeze             = flag.String("unfreeze", "", "unfreeze filterconfig from a frozen file")
	verbose              = flag.Bool("verbose", false, "verbose output")
	server               = flag.String("server", "", "if not empty, query SOLR to deduplicate on-the-fly")
	prefs                = flag.String("prefs", "85 55 89 60 50 105 34 101 53 49 28 48 121", "most preferred source id first, for deduplication")
	ignoreSameIdentifier = flag.Bool("isi", false, "when doing deduplication, ignore matches in index with the same id")
)

// SelectResponse with reduced fields.
type SelectResponse struct {
	Response struct {
		Docs []struct {
			ID          string   `json:"id"`
			Institution []string `json:"institution"`
			SourceID    string   `json:"source_id"`
		} `json:"docs"`
		NumFound int64 `json:"numFound"`
		Start    int64 `json:"start"`
	} `json:"response"`
	ResponseHeader struct {
		Params struct {
			Q  string `json:"q"`
			Wt string `json:"wt"`
		} `json:"params"`
		QTime  int64
		Status int64 `json:"status"`
	} `json:"responseHeader"`
}

// stringSliceContains returns true, if a given string is contained in a slice.
func stringSliceContains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

// preferencePosition returns the position of a given preference as int.
// Smaller means preferred. If there is no match, return some higher number
// (low prio).
func preferencePosition(sid string) int {
	fields := strings.Fields(*prefs)
	for pos, v := range fields {
		v = strings.TrimSpace(v)
		if v == sid {
			return pos
		}
	}
	return 1000 // Or anything higher than the number of sources.
}

// DroppableLabels returns a list of labels, that can be dropped with regard to
// an index. If document has no DOI, there is nothing to return.
func DroppableLabels(is finc.IntermediateSchema) (labels []string, err error) {
	doi := strings.TrimSpace(is.DOI)
	if doi == "" {
		return
	}
	link := fmt.Sprintf(`%s/select?df=allfields&wt=json&q="%s"`, *server, url.QueryEscape(doi))
	if *verbose {
		log.Println(link)
	}
	resp, err := http.Get(link)
	if err != nil {
		return labels, err
	}
	defer resp.Body.Close()
	var sr SelectResponse

	// Keep response for debugging.
	var buf bytes.Buffer
	tee := io.TeeReader(resp.Body, &buf)

	if err := json.NewDecoder(tee).Decode(&sr); err != nil {
		log.Printf("failed link: %s", link)
		log.Printf("failed response: %s", buf.String())
		return labels, err
	}
	// Ignored merely counts the number of docs, that had the same id in the index, for logging.
	var ignored int
	for _, label := range is.Labels {
		// For each label (ISIL), see, whether any match in SOLR has the same
		// label (ISIL) as well.

		for _, doc := range sr.Response.Docs {
			if *ignoreSameIdentifier && doc.ID == is.ID {
				ignored++
				continue
			}
			if !stringSliceContains(doc.Institution, label) {
				continue
			}
			// The document (is) might be already in the index (same or other source).
			if preferencePosition(is.SourceID) >= preferencePosition(doc.SourceID) {
				// The prio position of the document is higher (means: lower prio). We may drop this label.
				labels = append(labels, label)
				break
			} else {
				log.Printf("%s (%s) has lower prio in index, but we cannot update index docs yet, skipping", is.ID, doi)
			}
		}
	}
	if ignored > 0 && *verbose {
		log.Printf("ignored %d docs", ignored)
	}
	return labels, nil
}

// removeEach returns a new slice with element not in a drop list.
func removeEach(ss []string, drop []string) (result []string) {
	for _, s := range ss {
		if !stringSliceContains(drop, s) {
			result = append(result, s)
		}
	}
	return
}

func main() {

	flag.Parse()

	if *version {
		fmt.Println(span.AppVersion)
		os.Exit(0)
	}

	if *config == "" && *unfreeze == "" {
		log.Fatal("config file required")
	}

	if *cpuProfile != "" {
		file, err := os.Create(*cpuProfile)
		if err != nil {
			log.Fatal(err)
		}
		pprof.StartCPUProfile(file)
		defer pprof.StopCPUProfile()
	}

	if *server != "" {
		*server = solrutil.PrependHTTP(*server)
	}

	// The configuration forest.
	var tagger filter.Tagger

	if *unfreeze != "" {
		dir, filterconfig, err := span.UnfreezeFilterConfig(*unfreeze)
		if err != nil {
			log.Fatal(err)
		}
		log.Printf("[span-tag] unfrooze filterconfig to: %s", filterconfig)
		defer os.RemoveAll(dir)
		*config = filterconfig
	}

	// Test, if we are given JSON directly.
	err := json.Unmarshal([]byte(*config), &tagger)
	if err != nil {
		// Fallback to parse config file.
		f, err := os.Open(*config)
		if err != nil {
			log.Fatal(err)
		}
		defer f.Close()
		if err := json.NewDecoder(f).Decode(&tagger); err != nil {
			log.Fatal(err)
		}
	}

	w := bufio.NewWriter(os.Stdout)
	defer w.Flush()

	var reader io.Reader = os.Stdin

	if flag.NArg() > 0 {
		var files []io.Reader
		for _, filename := range flag.Args() {
			f, err := os.Open(filename)
			if err != nil {
				log.Fatal(err)
			}
			defer f.Close()
			files = append(files, f)
		}
		reader = io.MultiReader(files...)
	}

	p := parallel.NewProcessor(bufio.NewReader(reader), w, func(_ int64, b []byte) ([]byte, error) {
		var is finc.IntermediateSchema
		if err := json.Unmarshal(b, &is); err != nil {
			return b, err
		}

		tagged := tagger.Tag(is)

		// Deduplicate against a SOLR.
		if *server != "" {
			droppable, err := DroppableLabels(tagged)
			if err != nil {
				log.Fatal(err)
			}
			if len(droppable) > 0 {
				before := len(tagged.Labels)
				tagged.Labels = removeEach(tagged.Labels, droppable)
				if *verbose {
					log.Printf("[%s] from %d to %d labels: %s",
						is.ID, before, len(tagged.Labels), tagged.Labels)
				}
			}
		}

		bb, err := json.Marshal(tagged)
		if err != nil {
			return bb, err
		}
		bb = append(bb, '\n')
		return bb, nil
	})

	p.NumWorkers = *numWorkers
	p.BatchSize = *size

	if err := p.Run(); err != nil {
		log.Fatal(err)
	}
}
