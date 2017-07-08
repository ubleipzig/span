// redact intermediate schema
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"runtime"

	"github.com/miku/span"
	"github.com/miku/span/bytebatch"
	"github.com/miku/span/formats/finc"
)

func main() {
	showVersion := flag.Bool("v", false, "prints current program version")
	size := flag.Int("b", 20000, "batch size")
	numWorkers := flag.Int("w", runtime.NumCPU(), "number of workers")

	flag.Parse()

	if *showVersion {
		fmt.Println(span.AppVersion)
		os.Exit(0)
	}

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
		p := bytebatch.NewLineProcessor(r, os.Stdout, func(b []byte) ([]byte, error) {
			is := finc.IntermediateSchema{}

			if err := json.Unmarshal(b, &is); err != nil {
				log.Printf("failed to unmarshal: %s", string(b))
				return b, err
			}

			// Redact full text.
			is.Fulltext = ""

			bb, err := json.Marshal(is)
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
}
