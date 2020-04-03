// WIP: span-tagger will be a replacement of span-tag, with improvements:
//
// 1. Get rid of a filterconfig JSON format, only use AMSL discovery output
// (turned into an sqlite3 db, via span-amsl-discovery -db ...); that should
// get rid of siskin/amsl.py, span-tag, span-freeze and the whole span/filter
// tree.
//
// 2. Allow for updated file output or just TSV of attachments (which we could
// diff for debugging or other things).
//
// Usage:
//
//     $ span-amsl-discovery -db amsl.db -live https://live.server
//     $ taskcat AIIntermediateSchema | span-tagger -db amsl.db > tagged.ndj
//
// TODO:
//
// * [ ] cover all attachment modes from https://git.io/JvdmC
// * [ ] add tests
//
// Performance:
//
// Single threaded 170M records, about 4 hours, thanks to caching (but only
// about 10M/s); 210m29.179s for 173759327 records; 13G output.
package main

import (
	"archive/zip"
	"bufio"
	"crypto/sha1"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"runtime/pprof"
	"sort"
	"strings"
	"time"

	"github.com/adrg/xdg"
	_ "github.com/mattn/go-sqlite3"
	"github.com/sethgrid/pester"

	"github.com/jmoiron/sqlx"
	"github.com/miku/span/atomic"
	"github.com/miku/span/formats/finc"
	"github.com/miku/span/licensing"
	"github.com/miku/span/licensing/kbart"

	log "github.com/sirupsen/logrus"
)

var (
	force      = flag.Bool("f", false, "force all external referenced links to be downloaded")
	dbFile     = flag.String("db", "", "path to an sqlite3 file generated by span-amsl-discovery -db file.db ...")
	cpuprofile = flag.String("cpuprofile", "", "file to cpu profile")
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

// HFCache wraps access to holdings files.
type HFCache struct {
	// entries maps a link or filename (or any identifier) to a map from ISSN
	// to licensing entries.
	entries map[string]map[string][]licensing.Entry
}

// cacheFilename returns the path to the locally cached version of this URL.
func (c *HFCache) cacheFilename(hflink string) string {
	h := sha1.New()
	_, _ = io.WriteString(h, hflink)
	return filepath.Join(xdg.CacheHome, "span", fmt.Sprintf("%x", h.Sum(nil)))
}

// populate fills the entries map from a given URL.
func (c *HFCache) populate(hflink string) error {
	if _, ok := c.entries[hflink]; ok {
		return nil
	}
	var (
		filename = c.cacheFilename(hflink)
		dir      = path.Dir(filename)
	)
	if fi, err := os.Stat(dir); os.IsNotExist(err) {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	} else if !fi.IsDir() {
		return fmt.Errorf("expected cache directory at: %s", dir)
	}
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		if err := download(hflink, filename); err != nil {
			return err
		}
	}
	var (
		h       = new(kbart.Holdings)
		zr, err = zip.OpenReader(filename)
	)
	if err == nil {
		defer zr.Close()
		for _, f := range zr.File {
			rc, err := f.Open()
			if err != nil {
				return err
			}
			if _, err := h.ReadFrom(rc); err != nil {
				return err
			}
			rc.Close()
		}
	} else {
		f, err := os.Open(filename)
		if err != nil {
			return err
		}
		defer f.Close()
		if _, err := h.ReadFrom(f); err != nil {
			return err
		}
	}
	snm := h.SerialNumberMap()
	c.entries[hflink] = h.SerialNumberMap()
	if len(snm) == 0 {
		log.Printf("warning: %s may not be KBART", hflink)
	} else {
		log.Printf("parsed %s", hflink)
		log.Printf("parsed %d entries from %s (%d)",
			len(c.entries[hflink]), filename, len(c.entries))
	}
	return nil
}

// Covers returns true, if a holdings file, given by link or filename, covers
// the document. The cache takes care of downloading the file, if necessary.
func (c *HFCache) Covers(hflink string, doc *finc.IntermediateSchema) (ok bool, err error) {
	if err = c.populate(hflink); err != nil {
		return false, err
	}
	for _, issn := range append(doc.ISSN, doc.EISSN...) {
		for _, entry := range c.entries[hflink][issn] {
			err = entry.Covers(doc.RawDate, doc.Volume, doc.Issue)
			if err == nil {
				return true, nil
			}
		}
	}
	return false, nil
}

// download retrieves a link and saves its content atomically in filename.
func download(link, filename string) error {
	resp, err := pester.Get(link)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	return atomic.WriteFile(filename, b, 0644)
}

// cacheKey returns a key for a document, containing a subset (e.g. sid and
// collcetions) of fields, e.g.  to be used to cache subset of the about 250k
// rows currently in AMSL.
func cacheKey(doc *finc.IntermediateSchema) string {
	v := doc.MegaCollections
	sort.Strings(v)
	return doc.SourceID + "@" + strings.Join(v, "@")
}

// Labeler updates an intermediate schema document.
// We need mostly: ISIL, SourceID, MegaCollection, TechnicalCollectionID, HoldFileURI,
// EvaluateHoldingsFileForLibrary
type Labeler struct {
	dbFile  string
	db      *sqlx.DB
	cache   map[string][]ConfigRow
	hfcache *HFCache
}

// open opens the database connection, read-only.
func (l *Labeler) open() (err error) {
	if l.db == nil {
		l.db, err = sqlx.Connect("sqlite3", fmt.Sprintf("%s?ro=1", l.dbFile))
	}
	return
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
	if len(doc.MegaCollections) == 0 {
		// TODO: Why zero?
		return result, nil
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
	rows, err := l.matchingRows(doc)
	if err != nil {
		return err
	}
	var labels = make(map[string]struct{}) // ISIL to attach

	// TODO: Distinguish cases, e.g. with or w/o HF, https://git.io/JvdmC.
	for _, row := range rows {
		switch {
		case row.EvaluateHoldingsFileForLibrary == "no" && row.LinkToHoldingsFile != "":
			return fmt.Errorf("config provides holding file, but does not want to evaluate it: %v", row)
		case row.EvaluateHoldingsFileForLibrary == "yes" && row.LinkToHoldingsFile != "":
			ok, err := l.hfcache.Covers(row.LinkToHoldingsFile, doc)
			if err != nil {
				return err
			}
			if ok {
				labels[row.ISIL] = struct{}{}
			}
		case row.EvaluateHoldingsFileForLibrary == "no":
			labels[row.ISIL] = struct{}{}
		case row.ContentFileURI != "":
		default:
			return fmt.Errorf("none of the attachment modes match for %v", doc)
		}
	}
	var keys []string
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	fmt.Printf("%s\t%s\n", doc.ID, strings.Join(keys, ", "))
	return nil
}

func main() {
	flag.Parse()
	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			log.Fatal(err)
		}
		defer f.Close()
		if err := pprof.StartCPUProfile(f); err != nil {
			log.Fatal(err)
		}
		defer pprof.StopCPUProfile()
	}
	if *dbFile == "" {
		log.Fatal("we need a configuration database")
	}
	var (
		hfcache = &HFCache{entries: make(map[string]map[string][]licensing.Entry)}
		labeler = &Labeler{dbFile: *dbFile, hfcache: hfcache}
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
		// TODO: return ISIL
		if err := labeler.Label(&doc); err != nil {
			log.Fatal(err)
		}
		i++
	}
}
