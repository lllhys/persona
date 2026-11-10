package extension

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/i18n"
)

// Download fetches pkg into dir (a fresh staging directory) and checks its sha256; it returns the file and the
// name it goes by (for its type). A package in the cache (Cache) is taken from there. With a cache, every caller
// asking for the same package while it downloads waits for the one download, and it goes on into the cache when
// they went away (market_download_shared.go). A download that gets no data for StallAfter gives up
// (ErrDownloadStalled), one that keeps going past its budget (budgetFor) says it was too slow (ErrDownloadSlow).
func (m *Market) Download(ctx context.Context, pkg *MarketPackage, dir string) (string, string, error) {
	if pkg == nil {
		return "", "", i18n.Err("extension-market_security.package")
	}
	if digest, err := hex.DecodeString(pkg.SHA256); err != nil || len(digest) != sha256.Size {
		return "", "", i18n.Err("extension-market_security.package")
	}
	if err := ValidateMarketMirrors(MarketMirrors{RegistryMirror: m.RegistryMirror, CatalogMirror: m.CatalogMirror}); err != nil {
		return "", "", err
	}
	if pkg.Size < 0 || pkg.Size > PackageMost {
		return "", "", i18n.Err("extension-market_security.package")
	}
	var oci *ociReference
	if strings.HasPrefix(pkg.URL, "oci:") {
		ref, err := parseOCI(pkg.URL)
		if err != nil {
			return "", "", err
		}
		if pkg.Size <= 0 {
			return "", "", i18n.Err("extension-market_security.package")
		}
		oci = &ref
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", "", err
	}
	file := filepath.Join(dir, "download")
	if name, ok, err := m.fromCache(ctx, pkg, oci != nil, file); err != nil {
		return "", "", err
	} else if ok {
		return file, name, nil
	}
	if m.Cache != "" {
		name, err := m.sharedFetch(ctx, pkg, oci, file)
		if err != nil {
			return "", "", err
		}
		return file, name, nil
	}
	ctx, cancel := context.WithTimeoutCause(ctx, m.totalBudget(pkg), errOverBudget)
	defer cancel()
	name, err := m.fetch(ctx, pkg, oci, file)
	if err != nil {
		return "", "", err
	}
	return file, name, nil
}

// DownloadBudget is how long a package's download may take at least. One that stops is caught by StallAfter; this
// bounds one that keeps trickling. It was five minutes, which a 13 MB package over a 33 KB/s link never made: it was
// cut at 10 MB every time (#3692). A variable for the tests.
var DownloadBudget = 30 * time.Minute

// budgetFor is the time pkg's download has: DownloadBudget, or longer for a package too big to arrive in it at
// 16 KiB/s.
func budgetFor(pkg *MarketPackage) time.Duration {
	return max(DownloadBudget, time.Duration(pkg.Size/(16<<10))*time.Second)
}

// totalBudget is budgetFor and the registry mirror's try before it.
func (m *Market) totalBudget(pkg *MarketPackage) time.Duration {
	if m.RegistryMirror != "" {
		return budgetFor(pkg) + mirrorFetchFor
	}
	return budgetFor(pkg)
}

// ErrDownloadSlow: a download that kept getting data but did not finish within its budget.
var ErrDownloadSlow = errors.New("download too slow")

// errOverBudget is the cause of a download's context when its own budget ran out, not its caller's.
var errOverBudget = errors.New("over its budget")

// fromCache puts the cached copy of pkg at file when the cache has a good one (ok); one that is not what it should be
// is removed, and it is downloaded again.
func (m *Market) fromCache(ctx context.Context, pkg *MarketPackage, oci bool, file string) (name string, ok bool, err error) {
	from, name := cached(m.Cache, pkg.SHA256)
	if from == "" {
		return "", false, nil
	}
	validName := true
	if oci {
		name = path.Base(name)
		validName = ociTitle.MatchString(name)
	}
	if err := os.Link(from, file); err != nil {
		if err := copyFile(from, file, 0o600); err != nil {
			return "", false, err
		}
	}
	fi, statErr := os.Stat(file)
	if got, err := fileSum(file); err == nil && strings.EqualFold(got, pkg.SHA256) && validName && statErr == nil && fi.Size() <= PackageMost && (pkg.Size <= 0 || fi.Size() == pkg.Size) {
		report(ctx, StepDownload, StepEvent{State: StepDone, Bytes: fi.Size(), Total: fi.Size(), Cached: true})
		reportState(ctx, StepVerify, StepDone)
		return name, true, nil
	}
	_ = os.Remove(from) // not what it should be: download it again
	_ = os.Remove(file)
	return "", false, nil
}

// fetch downloads pkg to file and checks it against its sha256 and size: from the registry mirror first when there is
// one (an OCI package), then from where the index says. ctx bounds the whole of it.
func (m *Market) fetch(ctx context.Context, pkg *MarketPackage, oci *ociReference, file string) (string, error) {
	attempts := []bool{false}
	if oci != nil && m.RegistryMirror != "" {
		attempts = []bool{true, false}
	}
	var name string
	var lastErr error
	for _, mirror := range attempts {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		budget := budgetFor(pkg)
		if mirror {
			budget = mirrorFetchFor
		}
		attemptCtx, cancelAttempt := context.WithTimeoutCause(ctx, budget, errOverBudget)
		// Truncate the staging file and recreate every writer, hash and watchdog.
		f, err := os.OpenFile(file, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			cancelAttempt()
			return "", err
		}
		h := sha256.New()
		dctx, pw, stop := watchDownload(attemptCtx, io.MultiWriter(f, h), pkg.Size)
		if oci != nil {
			name, err = m.downloadOCI(dctx, *oci, mirror, pw)
		} else {
			name, err = m.download(dctx, pkg.URL, pw)
		}
		stalled := stop()
		// Its own budget ran out (not its caller's): the link is too slow for it.
		late := errors.Is(context.Cause(attemptCtx), errOverBudget)
		closeErr := f.Close()
		cancelAttempt()
		if err == nil {
			err = closeErr
		}
		if err == nil && (pw.n > PackageMost || pkg.Size > 0 && pw.n != pkg.Size) {
			err = i18n.Err("extension-market_security.checksum")
		}
		if err == nil {
			reportState(ctx, StepVerify, StepRunning)
			if got := hex.EncodeToString(h.Sum(nil)); !strings.EqualFold(got, pkg.SHA256) {
				err = errOfKind(ErrChecksum, "%s", i18n.Err("extension-market_security.checksum"))
			}
		}
		if err != nil {
			lastErr = err
			switch {
			case stalled:
				m.client().CloseIdleConnections()
				lastErr = messageOfKind(ErrDownloadStalled, "extension-market_security.stalled")
			case late && !mirror:
				m.client().CloseIdleConnections()
				lastErr = messageOfKind(ErrDownloadSlow, "extension-market_security.too_slow",
					i18n.Args{"minutes": int(budget / time.Minute), "got": sizeText(pw.n, pkg.Size)})
			}
			_ = os.Remove(file)
			continue
		}
		report(ctx, StepDownload, StepEvent{State: StepDone, Bytes: pw.n, Total: max(pw.n, pkg.Size)})
		reportState(ctx, StepVerify, StepDone)
		return name, nil
	}
	return "", lastErr
}

// sizeText is how much of a download arrived, for its error: "10.1 MB of 13.0 MB" (the size unknown: "10.1 MB").
func sizeText(got, total int64) string {
	mb := func(n int64) string { return fmt.Sprintf("%.1f MB", float64(n)/1e6) }
	if total > 0 {
		return mb(got) + " / " + mb(total)
	}
	return mb(got)
}

// fileSum is the sha256 of a file, in hex.
func fileSum(file string) (string, error) {
	f, err := os.Open(file)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func (m *Market) download(ctx context.Context, ref string, w io.Writer) (string, error) {
	const octet = "application/octet-stream"
	switch {
	case strings.HasPrefix(ref, "https://"):
		u, _ := url.Parse(ref)
		return u.Path, m.get(ctx, ref, octet, w, PackageMost)
	case strings.HasPrefix(ref, "release:"):
		if m.Spec.Kind != "github" {
			return "", errors.New("release: 只能用在 GitHub 市场")
		}
		tag, asset, ok := strings.Cut(strings.TrimPrefix(ref, "release:"), "/")
		if !ok || tag == "" || asset == "" {
			return "", errors.New("写成 release:<tag>/<附件名>")
		}
		var b strings.Builder
		rel := fmt.Sprintf("%s/repos/%s/%s/releases/tags/%s", m.api(), url.PathEscape(m.Spec.Owner), url.PathEscape(m.Spec.Repo), url.PathEscape(tag))
		if err := m.get(ctx, rel, "application/vnd.github+json", &b, IndexMost); err != nil {
			if _, web, ok := m.githubFallback(); ok && errors.Is(err, ErrRateLimited) {
				// The download link of a public release's asset is not the API's: no limit, the sha256 still checks it.
				u := fmt.Sprintf("%s/%s/%s/releases/download/%s/%s", web, url.PathEscape(m.Spec.Owner), url.PathEscape(m.Spec.Repo),
					url.PathEscape(tag), url.PathEscape(asset))
				if ferr := m.get(ctx, u, octet, w, PackageMost); ferr == nil {
					return asset, nil
				}
			}
			return "", fmt.Errorf("Release %s：%w", tag, err)
		}
		var r struct {
			Assets []struct {
				Name string `json:"name"`
				URL  string `json:"url"`
			} `json:"assets"`
		}
		if err := json.Unmarshal([]byte(b.String()), &r); err != nil {
			return "", err
		}
		for _, a := range r.Assets {
			if a.Name == asset {
				return asset, m.get(ctx, a.URL, octet, w, PackageMost)
			}
		}
		return "", fmt.Errorf("Release %s 里没有 %s", tag, asset)
	default:
		clean := path.Clean("/" + ref)[1:]
		if clean == "" || strings.HasPrefix(ref, "/") || clean != ref {
			return "", errors.New("包的相对路径不能跳出市场")
		}
		if m.Spec.Kind == "git" {
			// A catalog mirror supplies no repository files. A fresh Market used by
			// installation may also have an old checkout from a previous read.
			if err := m.syncGit(ctx); err != nil {
				return "", err
			}
			return clean, m.gitFile(clean, w, PackageMost)
		}
		if m.Spec.Kind == "github" {
			return clean, m.getRepoFile(ctx, clean, w, PackageMost)
		}
		base, _ := url.Parse(m.Spec.URL)
		u := base.ResolveReference(&url.URL{Path: clean})
		return u.Path, m.get(ctx, u.String(), octet, w, PackageMost)
	}
}
