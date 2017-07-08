package formeta

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/miku/span/formats/finc"
)

func TestEncoding(t *testing.T) {
	var cases = []struct {
		in  interface{}
		out string
		err error
	}{
		{in: "", out: "", err: nil},
		{in: "x", out: "", err: ErrValueNotAllowed},
		{in: struct{ A string }{A: "B"}, out: `{ A: 'B',  }`, err: nil},
		{in: struct{ A string }{A: "B 'A"}, out: `{ A: 'B \'A',  }`, err: nil},
		{in: struct{ A []string }{A: []string{"B", "C"}}, out: `{ A: 'B', A: 'C',  }`, err: nil},
		{in: struct{ A int }{A: 1}, out: `{ A: 1,  }`, err: nil},
		{in: struct{ A int64 }{A: 1}, out: `{ A: 1,  }`, err: nil},
		{
			in: struct{ A string }{A: `B
A`}, out: `{ A: 'B\nA',  }`, err: nil,
		},
		{
			in: struct{ A string }{A: `B\ A`}, out: `{ A: 'B\\ A',  }`, err: nil,
		},
		{
			in: struct{ A string }{A: `B\
'A \`}, out: `{ A: 'B\\\n\'A \\',  }`, err: nil,
		},
	}

	for _, c := range cases {
		b, err := Marshal(c.in)
		if err != c.err {
			t.Errorf("Marshal got %v, want %v", err, c.err)
		}
		if string(b) != c.out {
			t.Errorf("Marshal got %v, want %v", string(b), c.out)
		}
	}
}

type TestPosition struct {
	Longitude float64
	Latitude  float64
}

type TestPeak struct {
	Name     string `json:"name"`
	Location TestPosition
	Ascent   time.Time
	Variants []string
	Camps    []TestPosition
}

func TestNested(t *testing.T) {
	p := TestPeak{
		Name: "пик Сталина",
		Location: TestPosition{
			38.916667, 72.016667,
		},
		Variants: []string{
			"Ismoil Somoni Peak",
			"Қуллаи Исмоили Сомонӣ",
		},
		Camps: []TestPosition{
			{38.916667, 72.016667},
			{38.916667, 72.016667},
			{38.916667, 72.016667},
		},
	}

	want := `{ name: 'пик Сталина', Location { Longitude: 38.916667, Latitude: 72.016667,  } Ascent: '0001-01-01T00:00:00Z', Variants: 'Ismoil Somoni Peak', Variants: 'Қуллаи Исмоили Сомонӣ', Camps { Longitude: 38.916667, Latitude: 72.016667,  } Camps { Longitude: 38.916667, Latitude: 72.016667,  } Camps { Longitude: 38.916667, Latitude: 72.016667,  }  }`

	b, err := Marshal(p)
	if err != nil {
		t.Errorf(err.Error())
	}
	if string(b) != want {
		t.Errorf("Marshal got %v, want %v", string(b), want)
	}
}

func TestDanglingCR(t *testing.T) {
	var cases = []struct {
		in  string
		out string
		err error
	}{
		{
			in:  `{"finc.format":"ElectronicArticle","finc.mega_collection":"Japanese Society for Horticultural Science (CrossRef)","finc.record_id":"ai-49-aHR0cDovL2R4LmRvaS5vcmcvMTAuMjUwMy9ocmouMy4zMjk","finc.source_id":"49","ris.type":"EJOUR","rft.atitle":"多様な生息地から採取したギョウジャニンニク系統の萌芽期の早晩性およびRAPD分析による分類\r Variations on Sprouting Time and Classification by RAPD Analysis of Allium victorialis L. Clones Collected from Diverse Habitats","rft.epage":"332","rft.genre":"article","rft.issn":["1347-2658","1880-3571"],"rft.issue":"4","rft.jtitle":"Horticultural Research (Japan)","rft.tpages":"4","rft.pages":"329-332","rft.pub":["Japanese Society for Horticultural Science"],"rft.date":"2004-01-01","x.date":"2004-01-01T00:00:00Z","rft.spage":"329","rft.volume":"3","authors":[{"rft.aulast":"Inatomi","rft.aufirst":"Yoshihiro"},{"rft.aulast":"Murata","rft.aufirst":"Naho"},{"rft.aulast":"Nakano","rft.aufirst":"Hideki"},{"rft.aulast":"Tamura","rft.aufirst":"Haruto"},{"rft.aulast":"Suzuki","rft.aufirst":"Takashi"},{"rft.aulast":"Oosawa","rft.aufirst":"Katsuji"}],"doi":"10.2503/hrj.3.329","languages":["eng"],"url":["http://dx.doi.org/10.2503/hrj.3.329"],"version":"0.9","x.type":"journal-article"}`,
			out: `{ finc.format: 'ElectronicArticle', finc.mega_collection: 'Japanese Society for Horticultural Science (CrossRef)', finc.record_id: 'ai-49-aHR0cDovL2R4LmRvaS5vcmcvMTAuMjUwMy9ocmouMy4zMjk', finc.source_id: '49', ris.type: 'EJOUR', rft.atitle: '多様な生息地から採取したギョウジャニンニク系統の萌芽期の早晩性およびRAPD分析による分類  Variations on Sprouting Time and Classification by RAPD Analysis of Allium victorialis L. Clones Collected from Diverse Habitats', rft.epage: '332', rft.genre: 'article', rft.issn: '1347-2658', rft.issn: '1880-3571', rft.issue: '4', rft.jtitle: 'Horticultural Research (Japan)', rft.tpages: '4', rft.pages: '329-332', rft.pub: 'Japanese Society for Horticultural Science', rft.date: '2004-01-01', x.date: '2004-01-01T00:00:00Z', rft.spage: '329', rft.volume: '3', authors { rft.aulast: 'Inatomi', rft.aufirst: 'Yoshihiro',  } authors { rft.aulast: 'Murata', rft.aufirst: 'Naho',  } authors { rft.aulast: 'Nakano', rft.aufirst: 'Hideki',  } authors { rft.aulast: 'Tamura', rft.aufirst: 'Haruto',  } authors { rft.aulast: 'Suzuki', rft.aufirst: 'Takashi',  } authors { rft.aulast: 'Oosawa', rft.aufirst: 'Katsuji',  } doi: '10.2503/hrj.3.329', languages: 'eng', url: 'http://dx.doi.org/10.2503/hrj.3.329', version: '0.9', x.type: 'journal-article', x.oa: 'false',  }`,
			err: nil,
		},
	}

	for _, c := range cases {
		var v finc.IntermediateSchema
		if err := json.Unmarshal([]byte(c.in), &v); err != nil {
			t.Errorf(err.Error())
		}
		b, err := Marshal(v)
		if err != c.err {
			t.Errorf("got error %v, want %v", err, c.err)
		}
		if string(b) != c.out {
			t.Errorf("got %v, want %v", string(b), c.out)
		}
	}
}
