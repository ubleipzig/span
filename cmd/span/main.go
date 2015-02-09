package main

import (
	"bufio"

	"github.com/miku/span"
	"github.com/miku/span/crossref"
	"github.com/miku/span/holdings"

	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"runtime"
	"runtime/pprof"
	"sync"
)

type Options struct {
	Holdings span.IsilIssnHolding
}

// Worker receives batches of strings, parses, transforms and serializes them
func Worker(batches chan []string, out chan []byte, options Options, wg *sync.WaitGroup) {
	defer wg.Done()
	var doc crossref.Document
	for batch := range batches {
		for _, line := range batch {
			json.Unmarshal([]byte(line), &doc)
			schema, err := doc.ToSchema()
			if err != nil {
				log.Fatal(err)
			}
			schema.Institutions = doc.Institutions(options.Holdings)
			b, err := json.Marshal(schema)
			if err != nil {
				log.Fatal(err)
			}
			out <- b
		}
	}
}

// Collector collects docs and writes them out to stdout
func Collector(docs chan []byte, done chan bool) {
	f := bufio.NewWriter(os.Stdout)
	defer f.Flush()
	for b := range docs {
		f.Write(b)
		f.Write([]byte("\n"))
	}
	done <- true
}

func main() {
	batchSize := flag.Int("b", 25000, "batch size")
	cpuprofile := flag.String("cpuprofile", "", "write cpu profile to file")
	numWorkers := flag.Int("w", runtime.NumCPU(), "workers")
	version := flag.Bool("v", false, "prints current program version")

	hfile := flag.String("hfile", "", "path to a single ovid style holdings file fixed for DE-15")

	PrintUsage := func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [OPTIONS] CROSSREF.LDJ\n", os.Args[0])
		flag.PrintDefaults()
	}

	flag.Parse()

	runtime.GOMAXPROCS(*numWorkers)

	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			log.Fatal(err)
		}
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}

	if *version {
		fmt.Println(span.Version)
		os.Exit(0)
	}

	if flag.NArg() == 0 {
		PrintUsage()
		os.Exit(1)
	}

	var options Options
	if *hfile != "" {
		file, err := os.Open(*hfile)
		if err != nil {
			log.Fatal(err)
		}
		defer file.Close()
		reader := bufio.NewReader(file)
		hmap := holdings.HoldingsMap(reader)
		options.Holdings = make(span.IsilIssnHolding)
		options.Holdings["DE-15"] = hmap
	}

	ff, err := os.Open(flag.Arg(0))
	if err != nil {
		log.Fatal(err)
	}
	defer ff.Close()
	reader := bufio.NewReader(ff)

	batches := make(chan []string)
	docs := make(chan []byte)
	done := make(chan bool)

	go Collector(docs, done)

	var wg sync.WaitGroup
	for i := 0; i < runtime.NumCPU(); i++ {
		wg.Add(1)
		go Worker(batches, docs, options, &wg)
	}

	i := 0
	batch := make([]string, *batchSize)
	for {
		line, err := reader.ReadString('\n')
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatal(err)
		}
		batch = append(batch, line)
		if i == *batchSize-1 {
			batches <- batch
			batch = batch[:0]
			i = 0
		}
		i++
	}
	batches <- batch
	close(batches)
	wg.Wait()
	close(docs)
	<-done
}
