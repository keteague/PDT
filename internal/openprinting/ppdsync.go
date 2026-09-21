// Package openprinting mirrors the OpenPrinting PPD library
// (https://www.openprinting.org/download/PPD/) into PDT's own
// Drivers/macOS/OpenPrinting/<Manufacturer>/ folders - the flat per-
// manufacturer layout internal/driver's scanOpenPrintingPPDs reads.
//
// The site is a plain Apache directory index, so this walks its HTML
// listings rather than calling any API. Downloaded files keep the server's
// own modification time (Ken, 2026-09-20), and a small state file next to
// them records what was last fetched so a re-run only downloads what changed.
package openprinting

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	// DefaultBaseURL is the PPD library's root directory listing.
	DefaultBaseURL = "https://www.openprinting.org/download/PPD/"

	// UserAgent is an ordinary desktop-browser identification (Ken,
	// 2026-09-20) with a PDT token appended so the site's operators can still
	// tell what this traffic is.
	UserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36 PDT-PPD-Sync"

	// StateFileName sits in the destination root (Drivers/macOS/OpenPrinting).
	StateFileName = ".pdt-ppdsync.json"

	defaultConcurrency = 4
	maxAttempts        = 3
	maxErrorsKept      = 10
)

// Entry is one row of an Apache directory index.
type Entry struct {
	Name     string // decoded, without the trailing slash
	IsDir    bool
	Modified string // as listed, e.g. "2021-09-02 18:59" ("" when absent)
	Size     string // as listed, e.g. "164K" ("" for directories/absent)
}

var rowRe = regexp.MustCompile(`(?is)<tr>.*?<a href="([^"]*)">[^<]*</a></td>\s*<td[^>]*>\s*([0-9]{4}-[0-9]{2}-[0-9]{2} [0-9]{2}:[0-9]{2})?\s*</td>\s*<td[^>]*>\s*([^<]*?)\s*</td>`)

// ParseListing extracts the file and subdirectory rows from an Apache
// "Index of" page, skipping the sort links and the parent-directory row.
func ParseListing(page string) []Entry {
	var out []Entry
	for _, m := range rowRe.FindAllStringSubmatch(page, -1) {
		href := html.UnescapeString(m[1])
		if href == "" || strings.HasPrefix(href, "?") || strings.HasPrefix(href, "/") || strings.Contains(href, "://") {
			continue
		}
		isDir := strings.HasSuffix(href, "/")
		name, err := url.PathUnescape(strings.TrimSuffix(href, "/"))
		if err != nil || name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) {
			continue
		}
		size := strings.TrimSpace(html.UnescapeString(m[3]))
		if size == "-" || isDir {
			size = ""
		}
		out = append(out, Entry{Name: name, IsDir: isDir, Modified: m[2], Size: size})
	}
	return out
}

// langDirRe matches the per-language subfolders some manufacturers use
// (de/, es/, fr/ ...). Only English is mirrored: the PPD names already carry
// a language suffix that PDT's model cleanup strips, so the other languages
// would just show up as duplicate models.
var langDirRe = regexp.MustCompile(`^[a-z]{2}$`)

func skipDir(name string) bool { return langDirRe.MatchString(name) && name != "en" }

func isPPD(name string) bool {
	l := strings.ToLower(name)
	return strings.HasSuffix(l, ".ppd") || strings.HasSuffix(l, ".ppd.gz")
}

// normName folds a manufacturer/folder name for matching: the site's
// "KONICA_MINOLTA" and PDT's "Konica Minolta" must meet.
func normName(s string) string {
	return strings.ToLower(strings.NewReplacer(" ", "", "_", "", "-", "").Replace(s))
}

// Options configures Sync.
type Options struct {
	BaseURL       string // "" = DefaultBaseURL
	Client        *http.Client
	Manufacturers []string // PDT display names ("Konica Minolta")
	DestRoot      string   // Drivers/macOS/OpenPrinting
	Concurrency   int
	Progress      func(Progress)
}

// PlanFile is one file Sync is about to download. Size is estimated from the
// directory listing's rounded figure ("164K"); the real size replaces it once
// the download starts.
type PlanFile struct {
	Path string // "<Manufacturer folder>/<file name>"
	Size int64
}

// Progress is one status update:
//
//	Phase "listing":  Manufacturer is being checked on the site.
//	Phase "plan":     Plan is everything that needs downloading (possibly
//	                  empty), TotalFiles/TotalBytes its totals.
//	Phase "transfer": File is downloading (FileDone of FileTotal bytes);
//	                  Done*/Total* are the whole batch's running totals. Sent
//	                  as bytes arrive and once when the file completes.
type Progress struct {
	Phase        string
	Manufacturer string
	Plan         []PlanFile
	File         string
	FileDone     int64
	FileTotal    int64
	DoneFiles    int
	TotalFiles   int
	DoneBytes    int64
	TotalBytes   int64
}

var sizeRe = regexp.MustCompile(`^([0-9]+(?:\.[0-9]+)?)([KMG]?)$`)

// parseListedSize converts an Apache listing size ("164K", "1.2M", "512")
// to bytes, 0 when unparseable.
func parseListedSize(s string) int64 {
	m := sizeRe.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return 0
	}
	var v float64
	fmt.Sscanf(m[1], "%f", &v)
	switch m[2] {
	case "K":
		v *= 1024
	case "M":
		v *= 1024 * 1024
	case "G":
		v *= 1024 * 1024 * 1024
	}
	return int64(v)
}

// Result summarises a Sync.
type Result struct {
	Downloaded int
	Skipped    int
	Failed     int
	Errors     []string
}

type job struct {
	key      string // "<remote>/<name>", the state-file key
	url      string
	destDir  string
	name     string
	path     string // "<local folder name>/<name>", for progress display
	modified string
	size     string
}

// fileProgress tracks one file's download for the batch totals. counted is
// how many of its bytes are currently included in the batch's DoneBytes (so a
// failed attempt can be rolled back), est its listing-based size estimate.
type fileProgress struct {
	est      int64
	counted  int64
	total    int64
	adjusted bool
}

func stateValue(j job) string { return j.modified + "|" + j.size }

// Sync mirrors every requested manufacturer's PPDs into opt.DestRoot. Files
// never get deleted locally. The context cancels both listing and downloads;
// whatever finished before then is kept and recorded.
func Sync(ctx context.Context, opt Options) (Result, error) {
	var res Result
	base := opt.BaseURL
	if base == "" {
		base = DefaultBaseURL
	}
	if !strings.HasSuffix(base, "/") {
		base += "/"
	}
	baseURL, err := url.Parse(base)
	if err != nil {
		return res, err
	}
	client := opt.Client
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Minute}
	}
	workers := opt.Concurrency
	if workers < 1 {
		workers = defaultConcurrency
	}
	emit := func(p Progress) {
		if opt.Progress != nil {
			opt.Progress(p)
		}
	}
	if err := os.MkdirAll(opt.DestRoot, 0o755); err != nil {
		return res, err
	}

	// Which of the site's folders belong to which PDT manufacturer.
	emit(Progress{Phase: "listing", Manufacturer: "OpenPrinting"})
	top, err := list(ctx, client, baseURL)
	if err != nil {
		return res, fmt.Errorf("listing %s: %w", base, err)
	}

	stateFile := filepath.Join(opt.DestRoot, StateFileName)
	state := loadState(stateFile)
	var jobs []job
	for _, m := range opt.Manufacturers {
		var remote string
		for _, e := range top {
			if e.IsDir && normName(e.Name) == normName(m) {
				remote = e.Name
				break
			}
		}
		if remote == "" {
			continue
		}
		emit(Progress{Phase: "listing", Manufacturer: m})
		destDir := localFolder(opt.DestRoot, m)
		sweepPartials(destDir)
		found, err := walk(ctx, client, baseURL.JoinPath(remote+"/"), remote, destDir)
		if err != nil {
			if ctx.Err() != nil {
				return res, ctx.Err()
			}
			res.Failed++
			res.addError(fmt.Sprintf("%s: %v", m, err))
			continue
		}
		jobs = append(jobs, found...)
	}

	var todo []job
	for _, j := range jobs {
		if state[j.key] == stateValue(j) {
			if _, err := os.Stat(filepath.Join(j.destDir, j.name)); err == nil {
				res.Skipped++
				continue
			}
		}
		todo = append(todo, j)
	}

	plan := make([]PlanFile, len(todo))
	var totalBytes int64
	for i, j := range todo {
		est := parseListedSize(j.size)
		plan[i] = PlanFile{Path: j.path, Size: est}
		totalBytes += est
	}
	emit(Progress{Phase: "plan", Plan: plan, TotalFiles: len(todo), TotalBytes: totalBytes})

	var (
		mu        sync.Mutex
		doneFiles int
		doneBytes int64
		wg        sync.WaitGroup
		ch        = make(chan job)
	)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range ch {
				fp := &fileProgress{est: parseListedSize(j.size)}
				// Called as bytes arrive (fileDone of fileTotal, the latter
				// -1 when the server sent no length) and on a failed attempt
				// with fileDone -1 to roll that attempt's bytes back.
				hook := func(fileDone, fileTotal int64) {
					mu.Lock()
					defer mu.Unlock()
					if fileDone < 0 {
						doneBytes -= fp.counted
						fp.counted = 0
						return
					}
					doneBytes += fileDone - fp.counted
					fp.counted = fileDone
					if fileTotal >= 0 {
						fp.total = fileTotal
						if !fp.adjusted {
							// Swap the listing's rounded estimate for the
							// real size as soon as it's known.
							totalBytes += fileTotal - fp.est
							fp.adjusted = true
						}
					} else if fp.total < fileDone {
						fp.total = fileDone
					}
					emit(Progress{Phase: "transfer", File: j.path, FileDone: fileDone, FileTotal: fp.total,
						DoneFiles: doneFiles, TotalFiles: len(todo), DoneBytes: doneBytes, TotalBytes: totalBytes})
				}
				err := download(ctx, client, j, hook)

				mu.Lock()
				if err != nil {
					res.Failed++
					if ctx.Err() == nil {
						res.addError(fmt.Sprintf("%s: %v", j.name, err))
					}
				} else {
					res.Downloaded++
					state[j.key] = stateValue(j)
				}
				doneFiles++
				fileTotal := fp.total
				if fileTotal < fp.counted {
					fileTotal = fp.counted
				}
				emit(Progress{Phase: "transfer", File: j.path, FileDone: fileTotal, FileTotal: fileTotal,
					DoneFiles: doneFiles, TotalFiles: len(todo), DoneBytes: doneBytes, TotalBytes: totalBytes})
				mu.Unlock()
			}
		}()
	}
feed:
	for _, j := range todo {
		select {
		case <-ctx.Done():
			break feed
		case ch <- j:
		}
	}
	close(ch)
	wg.Wait()

	saveState(stateFile, state)
	if ctx.Err() != nil {
		return res, ctx.Err()
	}
	return res, nil
}

// sweepPartials removes .part files a previous run left behind (a killed
// process can't clean up after itself). A finished or canceled run never
// leaves one: see download.
func sweepPartials(dir string) {
	matches, _ := filepath.Glob(filepath.Join(dir, "*.part"))
	for _, m := range matches {
		_ = os.Remove(m)
	}
}

func (r *Result) addError(s string) {
	if len(r.Errors) < maxErrorsKept {
		r.Errors = append(r.Errors, s)
	}
}

// localFolder picks the destination folder for manufacturer m: an existing
// subfolder that already matches (whatever spacing it was created with), else
// a new one named exactly m.
func localFolder(root, m string) string {
	if entries, err := os.ReadDir(root); err == nil {
		for _, e := range entries {
			if e.IsDir() && normName(e.Name()) == normName(m) {
				return filepath.Join(root, e.Name())
			}
		}
	}
	return filepath.Join(root, m)
}

func list(ctx context.Context, client *http.Client, u *url.URL) ([]Entry, error) {
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			if err := sleepCtx(ctx, time.Duration(attempt)*time.Second); err != nil {
				return nil, err
			}
		}
		req, err := newRequest(ctx, u.String())
		if err != nil {
			return nil, err
		}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK && err == nil {
			return ParseListing(string(body)), nil
		}
		if err == nil {
			err = fmt.Errorf("HTTP %s", resp.Status)
			if resp.StatusCode >= 400 && resp.StatusCode < 500 {
				return nil, err
			}
		}
		lastErr = err
	}
	return nil, lastErr
}

// walk collects every PPD under dir (recursing into subfolders except the
// non-English language ones), flattened into destDir. A name seen twice keeps
// the first occurrence, so top-level files win over a subfolder's copy.
func walk(ctx context.Context, client *http.Client, dir *url.URL, remote, destDir string) ([]job, error) {
	var out []job
	seen := map[string]bool{}
	queue := []*url.URL{dir}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		entries, err := list(ctx, client, cur)
		if err != nil {
			return out, err
		}
		sort.SliceStable(entries, func(i, k int) bool { return entries[i].Name < entries[k].Name })
		for _, e := range entries {
			if e.IsDir {
				if !skipDir(e.Name) {
					queue = append(queue, cur.JoinPath(e.Name+"/"))
				}
				continue
			}
			if !isPPD(e.Name) || strings.HasPrefix(e.Name, "._") || seen[e.Name] {
				continue
			}
			seen[e.Name] = true
			out = append(out, job{
				key:      path.Join(remote, e.Name),
				url:      cur.JoinPath(e.Name).String(),
				destDir:  destDir,
				name:     e.Name,
				path:     filepath.Base(destDir) + "/" + e.Name,
				modified: e.Modified,
				size:     e.Size,
			})
		}
	}
	return out, nil
}

func newRequest(ctx context.Context, u string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	return req, nil
}

// download fetches j into its destination folder via a .part file, then sets
// the modification time to the server's own (Last-Modified, falling back to
// the directory listing's time) so the local copy keeps the file's real date.
//
// A canceled or failed transfer never leaves a partial file behind: the bytes
// only ever land in the .part file, which is deleted on any error, and the
// real name appears (by rename) only once the whole file has arrived intact.
func download(ctx context.Context, client *http.Client, j job, hook func(fileDone, fileTotal int64)) error {
	if err := os.MkdirAll(j.destDir, 0o755); err != nil {
		return err
	}
	dest := filepath.Join(j.destDir, j.name)
	part := dest + ".part"
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			if err := sleepCtx(ctx, time.Duration(attempt)*time.Second); err != nil {
				return err
			}
		}
		mtime, retry, err := fetchTo(ctx, client, j, part, hook)
		if err == nil {
			if err := os.Rename(part, dest); err != nil {
				_ = os.Remove(part)
				return err
			}
			if !mtime.IsZero() {
				_ = os.Chtimes(dest, mtime, mtime)
			}
			return nil
		}
		_ = os.Remove(part)
		hook(-1, -1) // roll this attempt's bytes back out of the batch totals
		lastErr = err
		if !retry || ctx.Err() != nil {
			break
		}
	}
	return lastErr
}

func fetchTo(ctx context.Context, client *http.Client, j job, part string, hook func(fileDone, fileTotal int64)) (mtime time.Time, retry bool, err error) {
	req, err := newRequest(ctx, j.url)
	if err != nil {
		return time.Time{}, false, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return time.Time{}, true, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return time.Time{}, resp.StatusCode >= 500, fmt.Errorf("HTTP %s", resp.Status)
	}
	f, err := os.Create(part)
	if err != nil {
		return time.Time{}, false, err
	}
	n, copyErr := io.Copy(f, &progressReader{r: resp.Body, total: resp.ContentLength, hook: hook})
	closeErr := f.Close()
	if err := errors.Join(copyErr, closeErr); err != nil {
		return time.Time{}, true, err
	}
	if resp.ContentLength >= 0 && n != resp.ContentLength {
		return time.Time{}, true, fmt.Errorf("short download: %d of %d bytes", n, resp.ContentLength)
	}
	if lm := resp.Header.Get("Last-Modified"); lm != "" {
		if t, err := http.ParseTime(lm); err == nil {
			return t, false, nil
		}
	}
	if t, err := time.Parse("2006-01-02 15:04", j.modified); err == nil {
		return t, false, nil
	}
	return time.Time{}, false, nil
}

// progressReader reports bytes read so far to hook as they arrive.
type progressReader struct {
	r     io.Reader
	total int64
	read  int64
	hook  func(fileDone, fileTotal int64)
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	if n > 0 {
		p.read += int64(n)
		p.hook(p.read, p.total)
	}
	return n, err
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func loadState(p string) map[string]string {
	state := map[string]string{}
	if b, err := os.ReadFile(p); err == nil {
		_ = json.Unmarshal(b, &state)
	}
	if state == nil {
		state = map[string]string{}
	}
	return state
}

func saveState(p string, state map[string]string) {
	if b, err := json.MarshalIndent(state, "", " "); err == nil {
		_ = os.WriteFile(p, b, 0o644)
	}
}
