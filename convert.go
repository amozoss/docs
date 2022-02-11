package main

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

/*

fix external links
fix external images
fix `{% `stuff
fix menu collapsing
fix menu order

*/

const assetsDir = "_assets"

var menuMapping = map[string]topLevelMenu{
	"dcs/storage": {"Decentralized Cloud Storage", 10},
	"dcs/downloads": {"Downloads", 20},
	"dcs/getting-started": {"Getting Started", 30},
	"dcs/api-reference": {"SDK & Reference", 40},
	"dcs/how-tos": {"How To's", 50},
	"dcs/solution-architectures": {"Solution Architectures", 60},
	"dcs/concepts": {"Concepts", 70},
	"dcs/support": {"Support", 80},
	"dcs/billing-payment-and-accounts-1": {"Billing, Payment & Accounts", 90},

	"node/before-you-begin": {"Before You Begin", 10},
	"node/dependencies": {"Dependencies", 20},
	"node/setup": {"Setup", 30},
	"node/sno-applications": {"SNO Applications", 40},
	"node/resources": {"Resources", 50},
	"node/solution-architectures": {"Solution Architectures", 60},
}

func main() {
	// reset gitbook
	exec.Command("git", "worktree", "remove", "gitbook/dcs").Run()
	exec.Command("git", "worktree", "add", "gitbook/dcs", "origin/gitbook-sync").Run()
	exec.Command("git", "worktree", "remove", "gitbook/node").Run()
	exec.Command("git", "worktree", "add", "gitbook/node", "origin/gitbook-node-sync").Run()

	// cleanup previous run
	os.RemoveAll("content")
	os.Mkdir("content", 0755)

	// start conversion
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
		os.Exit(1)
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
	conv.AddSectionIndices()
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
		targetPath := path.Join(conv.ContentDir, conv.TargetDir, assetsDir, noPrefix)
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
	return writeFile(to, data)
}

func writeFile(to string, data []byte) error {
	if err := ensureFileDir(to); err != nil {
		return err
	}
	return os.WriteFile(to, data, 0644)
}

func trimPrefix(path, prefix string) string {
	return strings.TrimLeft(strings.TrimPrefix(path, prefix), "\\/")
}

func ensureFileDir(path string) error {
	return os.MkdirAll(filepath.Dir(path), 0755)
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
	return writeFile(path, []byte(strings.Join([]string{
		"",
		page.FrontMatter,
		page.Content,
	}, "---\n")))
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
			url = "/" + conv.TargetDir + "/" + assetsDir + url[p+8+7:]
		}
		if hasAngle {
			url = "<" + url
		}
		return "![" + title + "](" + url + ")"
	})
}

func (conv *Convert) AddSectionIndices() {
	entries, err := os.ReadDir(filepath.Join(conv.ContentDir, conv.TargetDir))
	if err != nil {
		conv.Failures = append(conv.Failures, err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if entry.Name() == assetsDir {
			continue
		}

		dir :=path.Join(conv.TargetDir, entry.Name())
		info, ok := menuMapping[dir]
		if !ok {
			conv.Failures = append(conv.Failures, fmt.Errorf("menu mapping missing for %s", dir))
		}

		content := "---\n"
		if info.title != "" {
			content += "title: \"" + info.title +"\"\n"
			content += "weight: " + strconv.Itoa(info.weight) + "\n"
		}
		content += "bookFlatSection: true\n"
		content += "---\n"

		if err := writeFile(path.Join(conv.ContentDir, dir, "_index.md"), []byte(content)); err != nil {
			conv.Failures = append(conv.Failures, fmt.Errorf("menu failed for %s", dir))
		}
	}
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
