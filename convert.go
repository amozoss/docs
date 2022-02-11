package main

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

/*

fix external links
fix external images
fix `{% `stuff
fix menu collapsing
fix menu order

*/

func main() {
	// cleanup previous run
	os.RemoveAll("content")
	os.Mkdir("content", 0644)

	failures := []error{}
	convs := []Convert{
		{
			SourceDir:  "gitbook/dcs/docs",
			ContentDir: "content",
			TargetDir:  "dcs",
		},
		{
			SourceDir:  "gitbook/node",
			ContentDir: "content",
			TargetDir:  "node",
		},
	}

	for _, conv := range convs {
		fmt.Println("# Converting", conv.SourceDir)
		conv.Run()
		failures = append(failures, conv.Failures...)
	}
	if len(failures) > 0 {
		fmt.Println("# ERRORS")
		for _, fail := range failures {
			fmt.Println(fail)
		}
	}
}

type Convert struct {
	SourceDir  string
	ContentDir string
	TargetDir  string

	Failures []error
}

func (conv *Convert) Run() {
	conv.Files()
}

func (conv *Convert) Files() {
	err := filepath.WalkDir(conv.SourceDir,
		func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if filepath.Base(path) == ".git" {
				return nil
			}
			if err := conv.Convert(filepath.ToSlash(path)); err != nil {
				conv.Failures = append(conv.Failures,
					fmt.Errorf("failed to convert %s: %w", path, err))
			}
			return nil
		})
	if err != nil {
		conv.Failures = append(conv.Failures, err)
	}
}

func (conv *Convert) Convert(fullPath string) error {
	fmt.Println("  - ", fullPath)
	contentPath := trimPrefix(fullPath, conv.SourceDir)

	switch path.Ext(contentPath) {
	case ".png", ".jpg", ".svg", ".gif":
		if !strings.HasPrefix(contentPath, ".gitbook/assets") {
			return fmt.Errorf("don't know where to move")
		}
		noPrefix := trimPrefix(contentPath, ".gitbook/assets")
		targetPath := path.Join(conv.ContentDir, conv.TargetDir, "_assets", noPrefix)
		err := copyFile(fullPath, targetPath)
		if err != nil {
			return fmt.Errorf("failed to copy: %w", err)
		}
		return nil

	case ".md":
	default:
		return fmt.Errorf("don't know how to handle %q", path.Ext(contentPath))
	}

	// markdown handling
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return fmt.Errorf("failed to load: %w", err)
	}

	page := ParsePage(string(data))
	conv.LiftTitle(&page)
	conv.ReplaceTags(&page)
	conv.FixTrailingSpace(&page)
	conv.FixLinksToReadme(&page)
	conv.FixImageLinks(&page)

	targetPath := path.Join(conv.ContentDir, conv.TargetDir, contentPath)
	if strings.EqualFold(path.Base(targetPath), "README.md") {
		targetPath = targetPath[:len(targetPath)-len("README.md")] + "_index.md"
	}

	return page.WriteToFile(targetPath)
}

func copyFile(from, to string) error {
	data, err := os.ReadFile(from)
	if err != nil {
		return err
	}
	if err := ensureFileDir(to); err != nil {
		return err
	}
	return os.WriteFile(to, data, 0644)
}

func trimPrefix(path, prefix string) string {
	return strings.TrimLeft(strings.TrimPrefix(path, prefix), "\\/")
}

func ensureFileDir(path string) error {
	return os.MkdirAll(filepath.Dir(path), 0644)
}

type Page struct {
	FrontMatter string
	Content     string
}

func ParsePage(content string) Page {
	tokens := strings.Split(content, "---\n")
	if len(tokens) == 1 {
		return Page{Content: content}
	}

	return Page{
		FrontMatter: tokens[1],
		Content:     tokens[2],
	}
}

func (page *Page) WriteToFile(path string) error {
	content := strings.Join([]string{
		"",
		page.FrontMatter,
		page.Content,
	}, "---\n")
	if err := ensureFileDir(path); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0644)
}

// LiftTitle moves `# XYZ` to front matter `title: `
func (conv *Convert) LiftTitle(page *Page) {
	if match(`title\s*:`, page.FrontMatter) {
		return
	}

	const rxTitle = `#\s*([A-Za-z0-9\- :]+)\n`

	var title string
	ok := match(rxTitle, page.Content, nil, &title)
	if !ok {
		return
	}

	page.FrontMatter = "title: \"" + title + "\"\n" + page.FrontMatter
	// hugo-book does not add the title automatically
	// page.Content = mustReplaceFirst("\n?"+rxTitle, page.Content, "")
}

// ReplaceTags implements replacing tags of `{% *** %}`
func (conv *Convert) ReplaceTags(page *Page) {
	rxTag := mustCompile(`{%\s*([a-zA-Z0-9-]+)\s(.*)\s*%}`)
	page.Content = rxTag.ReplaceAllStringFunc(page.Content, func(tag string) string {
		tok := rxTag.FindStringSubmatch(tag)
		switch tok[1] {
		case "tabs":
			return `{{< tabs >}}`
		case "endtabs":
			return `{{< /tabs >}}`
		case "tab":
			var title string
			if match(`^title="(.*)"$`, strings.TrimSpace(tok[2]), nil, &title) {
				return `{{< tab "` + strings.TrimSpace(title) + `" >}}`
			}
		case "endtab":
			return `{{< /tab >}}`
		case "hint":
			switch strings.TrimSpace(tok[2]) {
			case `style="info"`:
				return `{{< hint info >}}`
			case `style="warning"`:
				return `{{< hint warning >}}`
			case `style="danger"`:
				return `{{< hint danger >}}`
			case `style="success"`:
				// TODO: add colors for it
				return `{{< hint success >}}`
			}
		case "endhint":
			return `{{< /hint >}}`
		case "embed":
			var url string
			if match(`^url="(.*)"$`, strings.TrimSpace(tok[2]), nil, &url) {
				// TODO: fetch link title
				// TODO: replace with youtube link
				return `{{< embed href="` + strings.TrimSpace(url) + `" >}}` + url + `{{< /biglink >}}`
			}
		case "endembed":
			// TODO: needs special case
			return ``
		case "content-ref":
			// Fix {% content-ref url="billing-and-payment.md" %} -->
			// TODO: needs special case
			return ``
		case "endcontent-ref":
			// TODO: needs special case
			return ``
		}

		panic("unhandled: " + tag)
	})
}

// FixTrailingSpace fixes some weird content issues in the markdown files.
func (conv *Convert) FixTrailingSpace(page *Page) {
	page.Content = replaceAll(` ?&#x20;`, page.Content, "")
	page.Content = replaceAll(` *$`, page.Content, "")
}

// FixLinksToReadme fixes links to README.md -> _index.md.
func (conv *Convert) FixLinksToReadme(page *Page) {
	page.Content = replaceAll(`README\.md\)`, page.Content, "_index.md)")
}

// FixImageLinks fixes links to "![xyz](<a/b/c>)" --> "![xyz](a/b/c)".
func (conv *Convert) FixImageLinks(page *Page) {
	rx := mustCompile(`!\[([^\]]*)\]\((<[^>]*>|[^\)]*)\)`)
	page.Content = rx.ReplaceAllStringFunc(page.Content, func(m string) string {
		match := rx.FindStringSubmatch(m)
		title, url := match[1], match[2]
		url = strings.ReplaceAll(url, "\\_", "_")

		hasAngle := url[0] == '<'
		if hasAngle {
			url = url[1:]
		}

		p := strings.Index(url, ".gitbook/assets")
		if p >= 0 {
			url = "/" + conv.TargetDir + "/_assets" + url[p+8+7:]
		}
		if hasAngle {
			url = "<" + url
		}
		return "![" + title + "](" + url + ")"
	})
}

var rxCache = map[string]*regexp.Regexp{}

func match(regex, content string, submatch ...*string) bool {
	rx := mustCompile(regex)
	matches := rx.FindStringSubmatch(content)
	if len(matches) == 0 {
		return false
	}
	if len(submatch) == 0 { // ignore when we don't want submatches
		return true
	}

	if len(submatch) != len(matches) {
		panic("match count mismatch")
	}

	for i, v := range matches {
		p := submatch[i]
		if p == nil {
			continue
		}
		*p = v
	}

	return true
}

func replaceAll(regex, content, newContent string) string {
	rx := mustCompile(regex)
	return rx.ReplaceAllString(content, newContent)
}

func mustReplaceFirst(regex, content, newContent string) string {
	rx := mustCompile(regex)
	loc := rx.FindStringIndex(content)
	if len(loc) == 0 {
		panic("did not match")
	}

	return content[:loc[0]] + newContent + content[loc[1]:]
}

func mustCompile(s string) *regexp.Regexp {
	rx, ok := rxCache[s]
	if !ok {
		rx = regexp.MustCompile(s)
		rxCache[s] = rx
	}
	return rx
}

type topLevelMenu struct {
	title  string
	weight int
}

var menuMapping = map[string]topLevelMenu{}
