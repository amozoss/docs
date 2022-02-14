package main

import (
	"flag"
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

*/

const assetsDir = "_assets"

var menuMapping = map[string]topLevelMenu{
	"dcs/storage":                        {"Decentralized Cloud Storage", 10},
	"dcs/downloads":                      {"Downloads", 20},
	"dcs/getting-started":                {"Getting Started", 30},
	"dcs/api-reference":                  {"SDK & Reference", 40},
	"dcs/how-tos":                        {"How To's", 50},
	"dcs/solution-architectures":         {"Solution Architectures", 60},
	"dcs/concepts":                       {"Concepts", 70},
	"dcs/support":                        {"Support", 80},
	"dcs/billing-payment-and-accounts-1": {"Billing, Payment & Accounts", 90},

	"node/before-you-begin":       {"Before You Begin", 10},
	"node/dependencies":           {"Dependencies", 20},
	"node/setup":                  {"Setup", 30},
	"node/sno-applications":       {"SNO Applications", 40},
	"node/resources":              {"Resources", 50},
	"node/solution-architectures": {"Solution Architectures", 60},
}

var urlToTitle = map[string]string{
	"https://docs.microsoft.com/en-us/windows-server/administration/openssh/openssh_install_firstuse":                                           "Get started with OpenSSH",
	"https://docs.microsoft.com/en-us/windows-server/administration/openssh/openssh_install_firstuse#installing-openssh-with-powershell":        "Install OpenSSH using Windows Settings",
	"https://docs.microsoft.com/en-us/windows-server/administration/openssh/openssh_server_configuration#windows-configurations-in-sshd_config": "Windows Configurations in sshd_config",
	"https://docs.microsoft.com/en-us/windows/wsl/install-win10":                                                                                "Install WSL",
	"https://osxdaily.com/2016/08/16/enable-ssh-mac-command-line/":                                                                              "How to Enable SSH on a Mac from the Command Line",
	"https://osxdaily.com/2016/08/16/enable-ssh-mac-command-line":                                                                               "How to Enable SSH on a Mac from the Command Line",
	"https://superuser.com/questions/364304/how-do-i-configure-ssh-on-os-x":                                                                     "How do I configure SSH on OS X?",
}

func run(exe string, args ...string) {
	cmd := exec.Command(exe, args...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	_ = cmd.Run()
}

func mustRun(exe string, args ...string) {
	cmd := exec.Command(exe, args...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	err := cmd.Run()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func main() {
	skipWorktree := flag.Bool("skip-worktree", false, "skips worktree logic")

	if *skipWorktree {
		// reset gitbook
		run("git", "worktree", "remove", "gitbook/dcs")
		mustRun("git", "worktree", "add", "gitbook/dcs", "origin/gitbook-sync")
		run("git", "worktree", "remove", "gitbook/node")
		mustRun("git", "worktree", "add", "gitbook/node", "origin/gitbook-node-sync")
	}

	// cleanup previous run
	os.RemoveAll("content")
	os.Mkdir("content", 0755)

	// start conversion
	failures := []error{}
	convs := []Convert{
		{
			SourceDir:  "gitbook/dcs/docs",
			ContentDir: "content",
			ExtraDir:   "content-extra",
			TargetDir:  "dcs",
		},
		{
			SourceDir:  "gitbook/node",
			ContentDir: "content",
			ExtraDir:   "content-extra",
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
	ExtraDir   string
	TargetDir  string

	OrderByFolder map[string][]SummaryItem

	Failures []error
}

type SummaryItem struct {
	Title       string
	ContentPath string
}

func (conv *Convert) Run() {
	conv.CreateOrder()
	conv.Files()
	conv.AddSectionIndices()
	conv.CopyExtra()
}

func (conv *Convert) Files() {
	err := filepath.WalkDir(conv.SourceDir,
		func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if filepath.Base(path) == ".git" {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if d.IsDir() {
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

func (conv *Convert) CopyExtra() {
	sourceDir := path.Join(conv.ExtraDir, conv.TargetDir)
	err := filepath.WalkDir(sourceDir,
		func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if filepath.Base(p) == ".git" {
				return filepath.SkipDir
			}
			if d.IsDir() {
				return nil
			}

			fullPath := filepath.ToSlash(p)
			fmt.Println("  - ", fullPath)
			contentPath := trimPrefix(fullPath, sourceDir)

			targetPath := path.Join(conv.ContentDir, conv.TargetDir, contentPath)
			err = copyFile(fullPath, targetPath)
			if err != nil {
				conv.Failures = append(conv.Failures, fmt.Errorf("failed to copy %q: %w", fullPath, err))
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
		switch contentPath {
		case ".gitbook/assets/0", ".gitbook/assets/1", ".gitbook/assets/2", ".gitbook/assets/3":
			noPrefix := contentPath[len(contentPath)-1:] + "-fix.png"
			targetPath := path.Join(conv.ContentDir, conv.TargetDir, assetsDir, noPrefix)
			err := copyFile(fullPath, targetPath)
			if err != nil {
				return fmt.Errorf("failed to copy: %w", err)
			}
			return nil
		}

		return fmt.Errorf("don't know how to handle %q", contentPath)
	}

	// markdown handling
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return fmt.Errorf("failed to load: %w", err)
	}

	page := ParsePage(contentPath, string(data))
	conv.AddWeight(&page)
	conv.LiftTitle(&page)
	conv.ReplaceContentRefs(&page)
	conv.ReplaceTags(&page)
	conv.FixTrailingSpace(&page)
	conv.FixLinksToReadme(&page)
	conv.FixImageLinks(&page)
	conv.FixRegularLinks(&page)
	conv.ReplaceMath(&page)
	conv.ReplaceStarryNight(&page)

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
	ContentPath string
	FrontMatter string
	Content     string
}

func ParsePage(contentPath, content string) Page {
	tokens := strings.SplitN(content, "---\n", 3)
	if len(tokens) == 1 {
		return Page{
			ContentPath: contentPath,
			Content:     content,
		}
	}

	return Page{
		ContentPath: contentPath,
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

func (conv *Convert) CreateOrder() {
	conv.OrderByFolder = map[string][]SummaryItem{}

	data, err := os.ReadFile(path.Join(conv.SourceDir, "SUMMARY.md"))
	if err != nil {
		conv.Failures = append(conv.Failures, fmt.Errorf("failed to read summary: %w", err))
		return
	}

	rx := mustCompile(`\[([^\]]*)\]\(([^)]*)\)`)
	for _, match := range rx.FindAllStringSubmatch(string(data), -1) {
		title := match[1]
		contentPath := match[2]

		dir := path.Dir(contentPath)
		if filepath.Base(contentPath) == "README.md" {
			dir = path.Dir(dir)
		}

		conv.OrderByFolder[dir] = append(conv.OrderByFolder[dir],
			SummaryItem{
				Title:       title,
				ContentPath: contentPath,
			})
	}
}

func (conv *Convert) AddWeight(page *Page) {
	if page.ContentPath == "SUMMARY.md" {
		page.FrontMatter = "draft: true\n" + page.FrontMatter
		return
	}

	dir := path.Dir(page.ContentPath)
	if filepath.Base(page.ContentPath) == "README.md" {
		dir = path.Dir(dir)
	}

	for i, item := range conv.OrderByFolder[dir] {
		if item.ContentPath == page.ContentPath {
			page.FrontMatter = "weight: " + strconv.Itoa(-100+i*10) + "\n" + page.FrontMatter
			return
		}
	}
	conv.Failures = append(conv.Failures, fmt.Errorf("order missing for %s", page.ContentPath))
}

// LiftTitle moves `# XYZ` to front matter `title: `
func (conv *Convert) LiftTitle(page *Page) {
	if match(`title\s*:`, page.FrontMatter) {
		return
	}

	const rxTitle = `#\s*([^\n]+)\n`

	var title string
	ok := match(rxTitle, page.Content, nil, &title)
	if !ok {
		return
	}

	page.FrontMatter = "title: \"" + title + "\"\n" + page.FrontMatter
	// hugo-book does not add the title automatically
	// page.Content = mustReplaceFirst("\n?"+rxTitle, page.Content, "")
}

// ReplaceContentRefs implements replacing multi-line content-ref tags:
//
//   {% content-ref url="before-you-begin/auth-token.md" %}
//   [auth-token.md](before-you-begin/auth-token.md)
//   {% endcontent-ref %}
func (conv *Convert) ReplaceContentRefs(page *Page) {
	rxContentRef := mustCompile(
		`{%\s+content-ref url="([^"]+)"\s+%}\n` +
			`\[([^\]]+)\]\(([^)]+)\)\n` +
			`{%\s+endcontent-ref\s+%}`)

	page.Content = rxContentRef.ReplaceAllStringFunc(page.Content, func(match string) string {
		matches := rxContentRef.FindStringSubmatch(match)
		url := matches[1]
		title := matches[2]
		link := matches[3]
		ref := strings.TrimSpace(url)
		expectedTitle := path.Base(link)

		if url == "broken-reference" {
			return `{{< biglink >}}Broken Reference{{< /biglink >}}`
		}
		if strings.HasSuffix(url, "/") {
			expectedTitle = path.Base(path.Dir(link))
			ref += "_index.md"
		}

		if url != link {
			panic(fmt.Sprintf("content-ref link mismatch: %s\nurl:%q\nlink:%q", match, url, link))
		}
		if title != expectedTitle {
			panic(fmt.Sprintf("content-ref title mismatch: %s\ntitle:%q\nexpected:%q", match, title, expectedTitle))
		}

		ref = conv.NearRef(page, ref)
		return `{{< biglink relref="` + ref + `" />}}`
	})
}

const youtubeLink = `{% embed url="https://www.youtube.com/watch?v=H6bRljVjR48" %}
Video Tutorial for the Setup Process
{% endembed %}`

// ReplaceTags implements replacing tags of `{% *** %}`
func (conv *Convert) ReplaceTags(page *Page) {
	tabIndex := 0

	if page.ContentPath == "sno-applications/qnap-storage-node-app.md" {
		page.Content = strings.ReplaceAll(page.Content, youtubeLink, "{{< youtube H6bRljVjR48 >}}")
	}

	rxTag := mustCompile(`{%\s*([a-zA-Z0-9-]+)\s(.*)\s*%}`)
	page.Content = rxTag.ReplaceAllStringFunc(page.Content, func(tag string) string {
		tok := rxTag.FindStringSubmatch(tag)
		switch tok[1] {
		case "tabs":
			tabIndex++
			return fmt.Sprintf(`{{< tabs id%d >}}`, tabIndex)
		case "endtabs":
			return `{{< /tabs >}}`
		case "tab":
			var title string
			if match(`^title="(.*)"$`, strings.TrimSpace(tok[2]), nil, &title) {
				title = strings.TrimSpace(title)
				if strings.EqualFold(title, "macOS") { // fix some misnamed tabs
					title = "macOS"
				}
				return `{{< tab "` + title + `" >}}`
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
				return `{{< hint success >}}`
			}
		case "endhint":
			return `{{< /hint >}}`
		case "embed":
			var url string
			if match(`^url="(.*)"$`, strings.TrimSpace(tok[2]), nil, &url) {
				url = strings.TrimSpace(url)
				title, ok := urlToTitle[url]
				if ok {
					return `{{< biglink href="` + strings.TrimSpace(url) + `" >}}` + title + `{{< /biglink >}}`
				}
			}
		}

		panic("unhandled: " + tag)
	})
}

func (conv *Convert) NearRef(page *Page, rel string) string {
	if strings.HasPrefix(rel, "http") || strings.HasPrefix(rel, "/") {
		return rel
	}
	if strings.HasPrefix(rel, "../") {
		return conv.AbsRef(page, rel)
	}
	return rel
}

func (conv *Convert) AbsRef(page *Page, rel string) string {
	if strings.HasPrefix(rel, "http") || strings.HasPrefix(rel, "/") {
		return rel
	}
	return "/" + path.Clean(path.Join(path.Dir(page.ContentPath), rel))
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
			url = url[1 : len(url)-1]
		}

		p := strings.Index(url, ".gitbook/assets")
		if p >= 0 {
			noPrefix := url[p+8+7:]
			// special case fix for images that are missing file extension
			if noPrefix == "/0" || noPrefix == "/1" || noPrefix == "/2" || noPrefix == "/3" {
				noPrefix += "-fix.png"
			}
			url = "/" + assetsDir + noPrefix
		}
		if hasAngle {
			url = "<" + url + ">"
		}
		return "![" + title + "](" + url + ")"
	})
}

// FixRegularLinks fixes links to "[xyz](<../b/c>)" --> "[xyz](/a/b/c)".
func (conv *Convert) FixRegularLinks(page *Page) {
	rx := mustCompile(`([^!])\[([^\]]*)\]\((<[^>]*>|[^\)]*)\)`)
	page.Content = rx.ReplaceAllStringFunc(page.Content, func(m string) string {
		match := rx.FindStringSubmatch(m)
		nonMatch, title, url := match[1], match[2], match[3]

		if strings.HasPrefix(url, "http") {
			return m
		}

		return nonMatch + "[" + title + "](" + conv.NearRef(page, url) + ")"
	})
}

// ReplaceMath replaces multiline $$\sqrt{}$$ with {{< katex >}}\sqrt{}{{< /katex >}}.
func (conv *Convert) ReplaceMath(page *Page) {
	page.Content = replaceAll(
		`\$\$\n(.*)\n\$\$`,
		page.Content,
		"{{< katex display >}}\n$1\n{{< /katex >}}",
	)
}

// ReplaceStarryNight replaces **** in a row, which seems a weird gitbook artifact.
func (conv *Convert) ReplaceStarryNight(page *Page) {
	page.Content = replaceAll(`( *\*\*\*\* +|( +\*\*\*\* *))`, page.Content, " ")
	page.Content = replaceAll(`\*\*\*\*`, page.Content, "")
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

		dir := path.Join(conv.TargetDir, entry.Name())
		info, ok := menuMapping[dir]
		if !ok {
			conv.Failures = append(conv.Failures, fmt.Errorf("menu mapping missing for %s", dir))
		}

		content := "---\n"
		if info.title != "" {
			content += "title: \"" + info.title + "\"\n"
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
