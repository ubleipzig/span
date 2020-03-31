// span-wip -db amsl.db < in > out
//
// For each record:
//
// [ ] select rows from db (sid, mc, tcid)
// [ ] if there is a holdingfile, and not cached, download, cache, parse and cache it
// [ ] match against holdingfile
// [ ] collect all ISIL and attach them (or just print id and isil)
// [ ] parallelize by copying the database (sqlite3 is single threaded), e.g. 4x NumCPU of the like
//
// Also, try to get rid of span-freeze by using some locally cached files (with
// expiry date).
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/jmoiron/sqlx"
	"github.com/miku/span/formats/finc"
)

var (
	force  = flag.Bool("f", false, "force all external referenced links to be downloaded")
	dbFile = flag.String("db", "", "path to an sqlite3 file generated by span-amsl-discovery -db file.db ...")
)

// ConfigRow decribing a single entry (e.g. an attachment request).
type ConfigRow struct {
	ShardLabel                     string
	ISIL                           string
	SourceID                       string
	TechnicalCollectionID          string
	MegaCollection                 string
	HoldingsFileURI                string
	HoldingsFileLabel              string
	LinkToHoldingsFile             string
	EvaluateHoldingsFileForLibrary string
	ContentFileURI                 string
	ContentFileLabel               string
	LinkToContentFile              string
	ExternalLinkToContentFile      string
	ProductISIL                    string
	DokumentURI                    string
	DokumentLabel                  string
}

func cacheKey(doc *finc.IntermediateSchema) string {
	return doc.SourceID + "@" + strings.Join(doc.MegaCollections, "@")
}

// Labeler updates an intermediate schema document.
// We need mostly: ISIL, SourceID, MegaCollection, TechnicalCollectionID, HoldFileURI,
// EvaluateHoldingsFileForLibrary
type Labeler struct {
	dbFile string
	db     *sqlx.DB

	cache map[string][]ConfigRow
}

// open opens the database connection, read-only.
func (l *Labeler) open() (err error) {
	if l.db != nil {
		return nil
	}
	dsn := fmt.Sprintf("%s?ro=1", l.dbFile)
	l.db, err = sqlx.Connect("sqlite3", dsn)
	if err != nil {
		return err
	}
	return nil
}

// matchingRows returns a list of relevant rows for a given document.
func (l *Labeler) matchingRows(doc *finc.IntermediateSchema) (result []ConfigRow, err error) {
	if l.cache == nil {
		l.cache = make(map[string][]ConfigRow)
	}
	key := cacheKey(doc)
	if v, ok := l.cache[key]; ok {
		return v, nil
	}
	// At a minimum, the sid and tcid or collection name must match.
	q, args, err := sqlx.In(`
		SELECT isil, sid, tcid, mc, hflink, hfeval, cflink, cfelink FROM amsl WHERE sid = ? AND (mc IN (?) OR tcid IN (?))
	`, doc.SourceID, doc.MegaCollections, doc.MegaCollections)
	if err != nil {
		return nil, err
	}
	rows, err := l.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var cr ConfigRow
		err = rows.Scan(&cr.ISIL,
			&cr.SourceID,
			&cr.TechnicalCollectionID,
			&cr.MegaCollection,
			&cr.LinkToHoldingsFile,
			&cr.EvaluateHoldingsFileForLibrary,
			&cr.ContentFileURI,
			&cr.ExternalLinkToContentFile)
		if err != nil {
			return nil, err
		}
		result = append(result, cr)
	}
	l.cache[key] = result
	return result, nil
}

// Label updates document in place.
func (l *Labeler) Label(doc *finc.IntermediateSchema) error {
	if err := l.open(); err != nil {
		return err
	}
	_, err := l.matchingRows(doc)
	if err != nil {
		return err
	}
	// Distinguish cases, e.g. with or w/o HF.
	return nil
}

func main() {
	flag.Parse()
	if *dbFile == "" {
		log.Fatal("we need a configuration database")
	}
	var (
		labeler = &Labeler{dbFile: *dbFile}
		br      = bufio.NewReader(os.Stdin)
		i       = 0
		started = time.Now()
	)
	for {
		if i%10000 == 0 {
			log.Printf("%d %0.2f", i, float64(i)/time.Since(started).Seconds())
		}
		b, err := br.ReadBytes('\n')
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatal(err)
		}
		var doc finc.IntermediateSchema // TODO: try reduced schema
		if err := json.Unmarshal(b, &doc); err != nil {
			log.Fatal(err)
		}
		if err := labeler.Label(&doc); err != nil {
			log.Fatal(err)
		}
		i++
	}
}
