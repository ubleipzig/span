package ceeol

import (
	"encoding/xml"
	"fmt"
	"strings"

	"time"

	"github.com/miku/span"
	"github.com/miku/span/finc"
	"github.com/shantanubhadoria/go-roman/roman"
)

const (
	SourceIdentifier = "53"
	Format           = "ElectronicArticle"
	Genre            = "article"
	DefaultRefType   = "EJOUR"
	Collection       = "CEEOL"
)

// Article from CEEOL, refs #9398.
type Article struct {
	XMLName                 xml.Name `xml:"Article"`
	UniqueID                string   `xml:"UniqueID"`
	ISSN                    string   `xml:"ISSN"`
	EISSN                   string   `xml:"eISSN"`
	PublicationTitle        string   `xml:"PublicationTitle"`
	PublicationTitleEnglish string   `xml:"PublicationTitleEnglish"`
	ArticleTitle            string   `xml:"ArticleTitle"`
	ArticleTitleEnglish     string   `xml:"ArticleTitleEnglish"`
	IsOpenAccess            string   `xml:"IsOpenAccess"`
	PublicationYear         string   `xml:"PublicationYear"`
	Volume                  string   `xml:"Volume"`
	Issue                   string   `xml:"Issue"`
	StartPage               string   `xml:"StartPage"`
	ArticleURL              string   `xml:"ArticleURL"`
	Authors                 []string `xml:"Authors>Author"`
	Languages               []string `xml:"Languages>Language"`
	ArticleSubtitle         string   `xml:"ArticleSubtitle"`
	PublicationSubtitle     string   `xml:"PublicationSubtitle"`
	EndPage                 string   `xml:"EndPage"`
	PageCount               string   `xml:"PageCount"`
	SubjectTerms            []string `xml:"SubjectTerms>SubjectTerm"`
	Publisher               string   `xml:"Publisher"`
	PublisherEnglish        string   `xml:"PublisherEnglish"`
	Keywords                string   `xml:"Keywords"`
	Description             string   `xml:"Description"`
	FileID                  string   `xml:"FileID"`
}

// ToIntermediateSchema converts an article to intermediate schema.
func (article *Article) ToIntermediateSchema() (*finc.IntermediateSchema, error) {
	output := finc.NewIntermediateSchema()
	if article.ArticleTitleEnglish == "" {
		output.ArticleTitle = article.ArticleTitle
	} else {
		output.ArticleTitle = fmt.Sprintf("%s [%s]", article.ArticleTitle, article.ArticleTitleEnglish)
	}
	output.ISSN = append(output.ISSN, article.ISSN)
	output.EISSN = append(output.EISSN, article.EISSN)
	v, err := roman.ToIndoArabic(article.Volume)
	if err != nil {
		output.Volume = article.Volume
	} else {
		output.Volume = fmt.Sprintf("%d", v)
	}
	if article.PublicationTitleEnglish == "" {
		output.JournalTitle = article.PublicationTitle
	} else {
		output.JournalTitle = fmt.Sprintf("%s [%s]", article.PublicationTitle, article.PublicationTitleEnglish)
	}
	if article.IsOpenAccess != "0" {
		output.OpenAccess = true
	}
	output.Issue = article.Issue
	output.StartPage = article.StartPage
	output.EndPage = article.EndPage
	output.PageCount = article.PageCount
	output.Abstract = article.Description
	output.Publishers = append(output.Publishers, article.Publisher)
	if article.PublisherEnglish != "" && article.PublisherEnglish != article.Publisher {
		output.Publishers = append(output.Publishers, article.PublisherEnglish)
	}
	for _, author := range article.Authors {
		output.Authors = append(output.Authors, finc.Author{Name: strings.TrimSpace(author)})
	}
	output.RawDate = fmt.Sprintf("%s-01-01", article.PublicationYear)
	output.Date, err = time.Parse("2006-01-02", fmt.Sprintf("%s-01-01", article.PublicationYear))
	output.Subjects = article.SubjectTerms
	output.URL = append(output.URL, article.ArticleURL)
	output.RecordID = fmt.Sprintf("ai-%s-%s", SourceIdentifier, article.UniqueID)
	output.SourceID = SourceIdentifier
	output.Format = Format
	output.Genre = Genre
	output.MegaCollection = Collection
	output.RefType = DefaultRefType
	for _, lang := range article.Languages {
		if isocode := span.LanguageIdentifier(lang); isocode != "" {
			output.Languages = append(output.Languages, isocode)
		}
	}
	return output, nil
}