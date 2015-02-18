package crossref

import (
	"fmt"
	"testing"

	"github.com/miku/span/holdings"
)

func TestCoveredBy(t *testing.T) {
	var tests = []struct {
		doc Document
		e   holdings.Entitlement
		err error
	}{
		{doc: Document{},
			e:   holdings.Entitlement{},
			err: nil},
		{doc: Document{Issued: DateField{DateParts: []DatePart{DatePart{2000}}}},
			e:   holdings.Entitlement{},
			err: nil},
		{doc: Document{Issued: DateField{DateParts: []DatePart{DatePart{2000, 1}}}},
			e:   holdings.Entitlement{},
			err: nil},
		{doc: Document{Issued: DateField{DateParts: []DatePart{DatePart{2000, 1, 1}}}},
			e:   holdings.Entitlement{},
			err: nil},
		{doc: Document{Issued: DateField{DateParts: []DatePart{DatePart{2000, 1, 1}}}},
			e:   holdings.Entitlement{FromYear: 1999, ToYear: 2001},
			err: nil},
		{doc: Document{},
			e:   holdings.Entitlement{FromYear: 1999, ToYear: 2001},
			err: nil},
		{doc: Document{},
			e:   holdings.Entitlement{FromYear: 1999},
			err: nil},
		{doc: Document{},
			e:   holdings.Entitlement{ToYear: 2001},
			err: nil},
		{doc: Document{Issued: DateField{DateParts: []DatePart{DatePart{2000, 1, 1}}}},
			e:   holdings.Entitlement{FromDelay: "-1Y"},
			err: nil},
		{doc: Document{Issued: DateField{DateParts: []DatePart{DatePart{2000, 1, 1}}}},
			e:   holdings.Entitlement{FromDelay: "-100Y"},
			err: fmt.Errorf("moving-wall violation")},
		{doc: Document{Issued: DateField{DateParts: []DatePart{DatePart{2000, 1, 1}}}},
			e:   holdings.Entitlement{FromYear: 2001},
			err: fmt.Errorf("from-year 2001 > 2000")},
		{doc: Document{Issued: DateField{DateParts: []DatePart{DatePart{2000, 1, 1}}}},
			e:   holdings.Entitlement{ToYear: 1999},
			err: fmt.Errorf("to-year 1999 < 2000")},
		{doc: Document{Volume: "1", Issued: DateField{DateParts: []DatePart{DatePart{2000, 1, 1}}}},
			e:   holdings.Entitlement{FromYear: 2000, FromVolume: 2},
			err: fmt.Errorf("from-volume 2 > 1")},
		{doc: Document{Volume: "2", Issued: DateField{DateParts: []DatePart{DatePart{2000, 1, 1}}}},
			e:   holdings.Entitlement{ToYear: 2000, ToVolume: 1},
			err: fmt.Errorf("to-volume 1 < 2")},
		{doc: Document{Volume: "1", Issue: "1", Issued: DateField{DateParts: []DatePart{DatePart{2000, 1, 1}}}},
			e:   holdings.Entitlement{FromYear: 2000, FromVolume: 1, FromIssue: 2},
			err: fmt.Errorf("from-issue 2 > 1")},
		{doc: Document{Volume: "1", Issue: "2", Issued: DateField{DateParts: []DatePart{DatePart{2000, 1, 1}}}},
			e:   holdings.Entitlement{ToYear: 2000, ToVolume: 1, ToIssue: 1},
			err: fmt.Errorf("to-issue 1 < 2")},
	}

	for _, tt := range tests {
		err := tt.doc.CoveredBy(tt.e)
		if err != tt.err {
			if err != nil && tt.err != nil {
				if err.Error() != tt.err.Error() {
					t.Errorf("got: %v, want: %s", err, tt.err)
				}
			} else {
				t.Errorf("got: %v, want: %s", err, tt.err)
			}
		}
	}
}
