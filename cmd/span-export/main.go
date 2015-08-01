// Converts intermediate schema docs into solr docs.
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"runtime"
	"runtime/pprof"
	"strings"
	"sync"

	"github.com/miku/span"
	"github.com/miku/span/container"
	"github.com/miku/span/filter"
	"github.com/miku/span/finc"
	"github.com/miku/span/finc/exporter"
)

// Options for worker.
type options struct {
	filters          []filter.Filter
	exportSchemaFunc func() finc.ExportSchema
	tagger           filter.ISILTagger
}

// Exporters holds available export formats
var Exporters = map[string]func() finc.ExportSchema{
	"dummy":       func() finc.ExportSchema { return new(exporter.DummySchema) },
	"solr4vu13v1": func() finc.ExportSchema { return new(exporter.Solr4Vufind13v1) },
	"solr4vu13v2": func() finc.ExportSchema { return new(exporter.Solr4Vufind13v2) },
	"solr4vu13v3": func() finc.ExportSchema { return new(exporter.Solr4Vufind13v3) },
}

// parseTagPathString turns TAG:/path/to into single strings and returns them.
func parseTagPathString(s string) (string, string, error) {
	p := strings.Split(s, ":")
	if len(p) != 2 {
		return "", "", errors.New("invalid tagpath, use ISIL:/path/to/file")
	}
	return p[0], p[1], nil
}

// parseTagPath returns the tag, an open file and possible errors.
func parseTagPath(s string) (string, *os.File, error) {
	var file *os.File
	isil, path, err := parseTagPathString(s)
	if err != nil {
		return isil, file, err
	}
	file, err = os.Open(path)
	if err != nil {
		return isil, file, err
	}
	return isil, file, nil
}

// worker iterates over string batches
func worker(queue chan []string, out chan []byte, opts options, wg *sync.WaitGroup) {
	defer wg.Done()
	for batch := range queue {
		for _, s := range batch {
			var err error
			is := finc.IntermediateSchema{}
			err = json.Unmarshal([]byte(s), &is)
			if err != nil {
				log.Fatal(err)
			}

			// Skip things, e.g. blacklisted DOIs.
			filtered := false
			for _, f := range opts.filters {
				if !f.Apply(is) {
					filtered = true
					break
				}
			}

			if filtered {
				continue
			}

			// Get export format.
			schema := opts.exportSchemaFunc()
			err = schema.Convert(is)
			if err != nil {
				log.Fatal(err)
			}

			// Get list of ISILs to attach.
			schema.Attach(opts.tagger.Tags(is))

			// TODO(miku): maybe move marshalling into Exporter, if we have
			// anything else than JSON - function could be somethings like
			// func Marshal() ([]byte, error)
			b, err := json.Marshal(schema)
			if err != nil {
				log.Fatal(err)
			}
			out <- b
		}
	}
}

func main() {

	var hfiles, lfiles, cfiles, any, source container.StringSlice
	flag.Var(&hfiles, "f", "ISIL:/path/to/ovid.xml")
	flag.Var(&lfiles, "l", "ISIL:/path/to/list.txt")
	flag.Var(&cfiles, "c", "ISIL:/path/to/collections.txt")
	flag.Var(&any, "any", "ISIL")
	flag.Var(&source, "source", "ISIL:SID")

	skip := flag.Bool("skip", false, "skip errors")
	showVersion := flag.Bool("v", false, "prints current program version")
	dumpFilters := flag.Bool("dump", false, "dump filters and exit")
	size := flag.Int("b", 20000, "batch size")
	numWorkers := flag.Int("w", runtime.NumCPU(), "number of workers")
	cpuprofile := flag.String("cpuprofile", "", "write cpu profile to file")
	format := flag.String("o", "solr4vu13v3", "output format")
	listFormats := flag.Bool("list", false, "list output formats")
	gzipOutput := flag.Bool("z", false, "gzip output")
	doiBlacklist := flag.String("doi-blacklist", "", "a list of DOIs to skip")

	flag.Parse()

	runtime.GOMAXPROCS(*numWorkers)

	if *showVersion {
		fmt.Println(span.AppVersion)
		os.Exit(0)
	}

	if *listFormats {
		for k := range Exporters {
			fmt.Println(k)
		}
		os.Exit(0)
	}

	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			log.Fatal(err)
		}
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}

	tagger := make(filter.ISILTagger)

	for _, s := range hfiles {
		isil, file, err := parseTagPath(s)
		if err != nil {
			log.Fatal(err)
		}
		defer file.Close()
		f, err := filter.NewHoldingFilter(file)
		if err != nil && !*skip {
			log.Fatal(err)
		}
		tagger[isil] = append(tagger[isil], f)
	}

	for _, s := range cfiles {
		isil, file, err := parseTagPath(s)
		if err != nil {
			log.Fatal(err)
		}
		defer file.Close()
		f, err := filter.NewCollectionFilter(file)
		if err != nil && !*skip {
			log.Fatal(err)
		}
		tagger[isil] = append(tagger[isil], f)
	}

	for _, s := range lfiles {
		isil, file, err := parseTagPath(s)
		if err != nil {
			log.Fatal(err)
		}
		defer file.Close()
		f, err := filter.NewListFilter(file)
		if err != nil && !*skip {
			log.Fatal(err)
		}
		tagger[isil] = append(tagger[isil], f)
	}

	for _, s := range source {
		ss := strings.Split(s, ":")
		if len(ss) != 2 {
			log.Fatal("use ISIL:SID")
		}
		isil, sid := ss[0], ss[1]
		tagger[isil] = append(tagger[isil], filter.SourceFilter{SourceID: sid})
	}

	for _, isil := range any {
		tagger[isil] = []filter.Filter{filter.Any{}}
	}

	if *dumpFilters {
		b, err := json.Marshal(tagger)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(string(b))
		os.Exit(0)
	}

	// TODO(miku): stutter less
	var filters []filter.Filter

	if *doiBlacklist != "" {
		file, err := os.Open(*doiBlacklist)
		if err != nil {
			log.Fatal(err)
		}
		f, err := filter.NewDOIFilter(bufio.NewReader(file))
		if err != nil {
			log.Fatal(err)
		}
		filters = append(filters, f)
	}

	exportSchemaFunc, ok := Exporters[*format]
	if !ok {
		log.Fatal("unknown export schema")
	}
	opts := options{tagger: tagger, exportSchemaFunc: exportSchemaFunc, filters: filters}

	queue := make(chan []string)
	out := make(chan []byte)
	done := make(chan bool)

	if *gzipOutput {
		go span.GzipSink(os.Stdout, out, done)
	} else {
		go span.ByteSink(os.Stdout, out, done)
	}

	var wg sync.WaitGroup

	for i := 0; i < *numWorkers; i++ {
		wg.Add(1)
		go worker(queue, out, opts, &wg)
	}

	var batch []string
	var i int

	var readers []io.Reader

	if flag.NArg() == 0 {
		readers = append(readers, os.Stdin)
	} else {
		for _, filename := range flag.Args() {
			file, err := os.Open(filename)
			if err != nil {
				log.Fatal(err)
			}
			defer file.Close()
			readers = append(readers, file)
		}
	}

	for _, r := range readers {
		br := bufio.NewReader(r)
		for {
			line, err := br.ReadString('\n')
			if err == io.EOF {
				break
			}
			if err != nil {
				log.Fatal(err)
			}
			batch = append(batch, line)
			if i%*size == 0 {
				b := make([]string, len(batch))
				copy(b, batch)
				queue <- b
				batch = batch[:0]
			}
			i++
		}
	}

	b := make([]string, len(batch))
	copy(b, batch)
	queue <- b

	close(queue)
	wg.Wait()
	close(out)
	<-done
}
