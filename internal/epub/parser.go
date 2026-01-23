package epub

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/minio/minio-go/v7"

	"github.com/johnnyr0x/reader-app/internal/models"
)

// Parser handles EPUB file parsing
type Parser struct {
	minioClient *minio.Client
	bucket      string
}

// NewParser creates a new EPUB parser
func NewParser(minioClient *minio.Client, bucket string) *Parser {
	return &Parser{
		minioClient: minioClient,
		bucket:      bucket,
	}
}

// container.xml structure
type container struct {
	Rootfiles []rootfile `xml:"rootfiles>rootfile"`
}

type rootfile struct {
	FullPath  string `xml:"full-path,attr"`
	MediaType string `xml:"media-type,attr"`
}

// OPF package structure
type opfPackage struct {
	Metadata opfMetadata `xml:"metadata"`
	Manifest opfManifest `xml:"manifest"`
	Spine    opfSpine    `xml:"spine"`
}

type opfMetadata struct {
	Title    string `xml:"title"`
	Creator  string `xml:"creator"`
	Language string `xml:"language"`
}

type opfManifest struct {
	Items []opfItem `xml:"item"`
}

type opfItem struct {
	ID         string `xml:"id,attr"`
	Href       string `xml:"href,attr"`
	MediaType  string `xml:"media-type,attr"`
	Properties string `xml:"properties,attr"`
}

type opfSpine struct {
	ItemRefs []opfItemRef `xml:"itemref"`
	Toc      string       `xml:"toc,attr"`
}

type opfItemRef struct {
	IDRef string `xml:"idref,attr"`
}

// NCX TOC structure (EPUB2)
type ncx struct {
	NavMap []navPoint `xml:"navMap>navPoint"`
}

type navPoint struct {
	Label     navLabel   `xml:"navLabel"`
	Content   navContent `xml:"content"`
	NavPoints []navPoint `xml:"navPoint"`
}

type navLabel struct {
	Text string `xml:"text"`
}

type navContent struct {
	Src string `xml:"src,attr"`
}

// Parse parses an EPUB file and returns book metadata
func (p *Parser) Parse(ctx context.Context, gutenbergID int) (*models.ParsedBook, error) {
	minioPath := fmt.Sprintf("%d/pg%d.epub", gutenbergID, gutenbergID)

	// Get EPUB from MinIO
	obj, err := p.minioClient.GetObject(ctx, p.bucket, minioPath, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get EPUB: %w", err)
	}
	defer obj.Close()

	// Read entire EPUB into memory for zip parsing
	data, err := io.ReadAll(obj)
	if err != nil {
		return nil, fmt.Errorf("failed to read EPUB: %w", err)
	}

	// Open as ZIP
	zipReader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("failed to open EPUB as ZIP: %w", err)
	}

	// Parse container.xml to find OPF
	opfPath, err := p.findOPFPath(zipReader)
	if err != nil {
		return nil, fmt.Errorf("failed to find OPF: %w", err)
	}

	// Parse OPF
	pkg, err := p.parseOPF(zipReader, opfPath)
	if err != nil {
		return nil, fmt.Errorf("failed to parse OPF: %w", err)
	}

	// Build book structure
	book := &models.ParsedBook{
		GutenbergID: gutenbergID,
		SKU:         fmt.Sprintf("BOOK-%d", gutenbergID),
		Title:       pkg.Metadata.Title,
		Author:      pkg.Metadata.Creator,
		Language:    pkg.Metadata.Language,
	}

	// Parse TOC
	opfDir := path.Dir(opfPath)
	book.TOC, err = p.parseTOC(zipReader, pkg, opfDir)
	if err != nil {
		// TOC parsing failure is not fatal
		book.TOC = []models.TOCEntry{}
	}

	// Build chapter list from spine
	book.Chapters = p.buildChapterList(zipReader, pkg, opfDir)

	// Map TOC entries to chapter indices based on href
	book.TOC = p.mapTOCToChapters(book.TOC, book.Chapters, opfDir)

	return book, nil
}

// mapTOCToChapters maps TOC entry hrefs to actual chapter indices
func (p *Parser) mapTOCToChapters(toc []models.TOCEntry, chapters []models.Chapter, opfDir string) []models.TOCEntry {
	// Build a map of href (without fragment) to chapter index
	hrefToChapter := make(map[string]int)
	for i, ch := range chapters {
		// Normalize the href
		href := ch.Href
		hrefToChapter[href] = i
	}

	// Update TOC entries with correct chapter indices
	for i := range toc {
		tocHref := toc[i].Href
		// Remove fragment (e.g., #section1)
		if idx := strings.Index(tocHref, "#"); idx != -1 {
			tocHref = tocHref[:idx]
		}

		// Try to find matching chapter
		if chIdx, ok := hrefToChapter[tocHref]; ok {
			toc[i].Index = chIdx
		} else {
			// Try with different path combinations
			for href, chIdx := range hrefToChapter {
				if strings.HasSuffix(href, tocHref) || strings.HasSuffix(tocHref, href) {
					toc[i].Index = chIdx
					break
				}
			}
		}
	}

	return toc
}

// GetChapter returns a specific chapter's content
func (p *Parser) GetChapter(ctx context.Context, gutenbergID int, chapterIndex int) (*models.Chapter, error) {
	book, err := p.Parse(ctx, gutenbergID)
	if err != nil {
		return nil, err
	}

	if chapterIndex < 0 || chapterIndex >= len(book.Chapters) {
		return nil, fmt.Errorf("chapter index out of range")
	}

	return &book.Chapters[chapterIndex], nil
}

func (p *Parser) findOPFPath(zipReader *zip.Reader) (string, error) {
	for _, f := range zipReader.File {
		if f.Name == "META-INF/container.xml" {
			rc, err := f.Open()
			if err != nil {
				return "", err
			}
			defer rc.Close()

			var c container
			if err := xml.NewDecoder(rc).Decode(&c); err != nil {
				return "", err
			}

			for _, rf := range c.Rootfiles {
				if rf.MediaType == "application/oebps-package+xml" {
					return rf.FullPath, nil
				}
			}
		}
	}
	return "", fmt.Errorf("container.xml not found")
}

func (p *Parser) parseOPF(zipReader *zip.Reader, opfPath string) (*opfPackage, error) {
	for _, f := range zipReader.File {
		if f.Name == opfPath {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()

			var pkg opfPackage
			if err := xml.NewDecoder(rc).Decode(&pkg); err != nil {
				return nil, err
			}

			return &pkg, nil
		}
	}
	return nil, fmt.Errorf("OPF file not found")
}

func (p *Parser) parseTOC(zipReader *zip.Reader, pkg *opfPackage, opfDir string) ([]models.TOCEntry, error) {
	// Find NCX file
	var ncxHref string
	for _, item := range pkg.Manifest.Items {
		if item.ID == pkg.Spine.Toc || item.MediaType == "application/x-dtbncx+xml" {
			ncxHref = item.Href
			break
		}
	}

	if ncxHref == "" {
		return nil, fmt.Errorf("NCX not found")
	}

	ncxPath := path.Join(opfDir, ncxHref)
	for _, f := range zipReader.File {
		if f.Name == ncxPath {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()

			var n ncx
			if err := xml.NewDecoder(rc).Decode(&n); err != nil {
				return nil, err
			}

			return p.flattenNavPoints(n.NavMap, 1), nil
		}
	}

	return nil, fmt.Errorf("NCX file not found")
}

func (p *Parser) flattenNavPoints(points []navPoint, level int) []models.TOCEntry {
	var entries []models.TOCEntry
	for _, np := range points {
		entries = append(entries, models.TOCEntry{
			Title: np.Label.Text,
			Href:  np.Content.Src,
			Level: level,
			Index: 0, // Will be set later based on href matching
		})
		// Recursively add nested entries
		entries = append(entries, p.flattenNavPoints(np.NavPoints, level+1)...)
	}
	return entries
}

func (p *Parser) buildChapterList(zipReader *zip.Reader, pkg *opfPackage, opfDir string) []models.Chapter {
	// Build ID to item map
	itemMap := make(map[string]opfItem)
	for _, item := range pkg.Manifest.Items {
		itemMap[item.ID] = item
	}

	var chapters []models.Chapter
	chapterNum := 0
	for _, ref := range pkg.Spine.ItemRefs {
		item, ok := itemMap[ref.IDRef]
		if !ok {
			continue
		}

		// Only include HTML content
		if !strings.Contains(item.MediaType, "html") && !strings.Contains(item.MediaType, "xhtml") {
			continue
		}

		chapterPath := path.Join(opfDir, item.Href)
		content := p.readFileContent(zipReader, chapterPath)

		// Try to extract title from content
		title := extractTitleFromContent(content)
		if title == "" {
			title = fmt.Sprintf("Section %d", chapterNum+1)
		}

		chapters = append(chapters, models.Chapter{
			Index:   chapterNum,
			Href:    item.Href,
			Title:   title,
			Content: content,
		})
		chapterNum++
	}

	return chapters
}

// extractTitleFromContent tries to find a title in the HTML content
func extractTitleFromContent(content string) string {
	// Try <title> tag first
	titleStart := strings.Index(strings.ToLower(content), "<title>")
	if titleStart != -1 {
		titleEnd := strings.Index(strings.ToLower(content[titleStart:]), "</title>")
		if titleEnd != -1 {
			title := content[titleStart+7 : titleStart+titleEnd]
			// Clean up the title
			title = strings.TrimSpace(title)
			if title != "" && !strings.Contains(strings.ToLower(title), "project gutenberg") {
				return title
			}
		}
	}

	// Try <h1> tag
	h1Start := strings.Index(strings.ToLower(content), "<h1")
	if h1Start != -1 {
		tagEnd := strings.Index(content[h1Start:], ">")
		if tagEnd != -1 {
			h1End := strings.Index(strings.ToLower(content[h1Start:]), "</h1>")
			if h1End != -1 {
				title := content[h1Start+tagEnd+1 : h1Start+h1End]
				// Strip any HTML tags within
				title = stripHTMLTags(title)
				if title != "" {
					return strings.TrimSpace(title)
				}
			}
		}
	}

	// Try <h2> tag
	h2Start := strings.Index(strings.ToLower(content), "<h2")
	if h2Start != -1 {
		tagEnd := strings.Index(content[h2Start:], ">")
		if tagEnd != -1 {
			h2End := strings.Index(strings.ToLower(content[h2Start:]), "</h2>")
			if h2End != -1 {
				title := content[h2Start+tagEnd+1 : h2Start+h2End]
				title = stripHTMLTags(title)
				if title != "" {
					return strings.TrimSpace(title)
				}
			}
		}
	}

	return ""
}

// stripHTMLTags removes HTML tags from a string
func stripHTMLTags(s string) string {
	result := s
	for {
		tagStart := strings.Index(result, "<")
		if tagStart == -1 {
			break
		}
		tagEnd := strings.Index(result[tagStart:], ">")
		if tagEnd == -1 {
			break
		}
		result = result[:tagStart] + result[tagStart+tagEnd+1:]
	}
	return result
}

func (p *Parser) readFileContent(zipReader *zip.Reader, filePath string) string {
	for _, f := range zipReader.File {
		if f.Name == filePath {
			rc, err := f.Open()
			if err != nil {
				return ""
			}
			defer rc.Close()

			data, err := io.ReadAll(rc)
			if err != nil {
				return ""
			}

			content := string(data)

			// Extract just the body content from HTML/XHTML
			content = extractBodyContent(content)

			return content
		}
	}
	return ""
}

// extractBodyContent extracts the content between <body> tags and sanitizes it
func extractBodyContent(html string) string {
	// Find body start
	bodyStart := strings.Index(strings.ToLower(html), "<body")
	if bodyStart == -1 {
		return html // No body tag, return as-is
	}

	// Find the end of the opening body tag
	bodyTagEnd := strings.Index(html[bodyStart:], ">")
	if bodyTagEnd == -1 {
		return html
	}
	bodyStart = bodyStart + bodyTagEnd + 1

	// Find body end
	bodyEnd := strings.LastIndex(strings.ToLower(html), "</body>")
	if bodyEnd == -1 {
		bodyEnd = len(html)
	}

	if bodyStart >= bodyEnd {
		return html
	}

	content := strings.TrimSpace(html[bodyStart:bodyEnd])

	// Remove elements that reference EPUB internal files (won't resolve in browser)
	content = removeRelativeImages(content) // <img> tags
	content = removeImageTags(content)      // <image> tags (SVG)
	content = removeSVGTags(content)        // <svg> blocks
	content = removeLinkTags(content)       // <link> tags (CSS)

	return content
}

// removeRelativeImages removes all img tags (EPUB internal images won't resolve)
func removeRelativeImages(html string) string {
	result := html
	for {
		imgStart := strings.Index(strings.ToLower(result), "<img")
		if imgStart == -1 {
			break
		}
		// Find the closing > (handle self-closing tags too)
		imgEnd := strings.Index(result[imgStart:], ">")
		if imgEnd == -1 {
			break
		}
		imgEnd = imgStart + imgEnd + 1

		// Remove the img tag entirely
		result = result[:imgStart] + result[imgEnd:]
	}
	return result
}

// removeLinkTags removes <link> tags
func removeLinkTags(html string) string {
	result := html
	for {
		linkStart := strings.Index(strings.ToLower(result), "<link")
		if linkStart == -1 {
			break
		}
		linkEnd := strings.Index(result[linkStart:], ">")
		if linkEnd == -1 {
			break
		}
		linkEnd = linkStart + linkEnd + 1
		result = result[:linkStart] + result[linkEnd:]
	}
	return result
}

// removeSVGTags removes <svg>...</svg> blocks (they often contain unresolvable images)
func removeSVGTags(html string) string {
	result := html
	for {
		svgStart := strings.Index(strings.ToLower(result), "<svg")
		if svgStart == -1 {
			break
		}
		svgEnd := strings.Index(strings.ToLower(result[svgStart:]), "</svg>")
		if svgEnd == -1 {
			// Self-closing or malformed, just remove opening tag
			tagEnd := strings.Index(result[svgStart:], ">")
			if tagEnd == -1 {
				break
			}
			result = result[:svgStart] + result[svgStart+tagEnd+1:]
		} else {
			svgEnd = svgStart + svgEnd + 6 // include </svg>
			result = result[:svgStart] + result[svgEnd:]
		}
	}
	return result
}

// removeImageTags removes <image> tags (SVG image elements)
func removeImageTags(html string) string {
	result := html
	for {
		imgStart := strings.Index(strings.ToLower(result), "<image")
		if imgStart == -1 {
			break
		}
		// Check for closing tag
		closeTag := strings.Index(strings.ToLower(result[imgStart:]), "</image>")
		selfClose := strings.Index(result[imgStart:], "/>")
		tagEnd := strings.Index(result[imgStart:], ">")

		if closeTag != -1 && (selfClose == -1 || closeTag < selfClose) {
			// Has closing tag
			result = result[:imgStart] + result[imgStart+closeTag+8:]
		} else if selfClose != -1 && selfClose < tagEnd+1 {
			// Self-closing
			result = result[:imgStart] + result[imgStart+selfClose+2:]
		} else if tagEnd != -1 {
			result = result[:imgStart] + result[imgStart+tagEnd+1:]
		} else {
			break
		}
	}
	return result
}
