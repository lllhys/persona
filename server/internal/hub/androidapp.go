package hub

import (
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/nwverse/persona/server/internal/i18n"
)

// The stable directory keeps its existing on-disk layout. Beta has its own
// signed-APK cache so version codes never keep a phone on the other channel.
func (h *Hub) androidUpdateDir() string {
	dir := filepath.Join(h.cfg.dir, "apps", "android")
	if h.upd != nil && h.upd.selectedBeta() {
		return filepath.Join(dir, "beta")
	}
	return dir
}

func (h *Hub) androidAppRoutes(api *http.ServeMux) {
	api.HandleFunc("GET /api/v1/app/android/latest", h.androidAppLatest)
	api.HandleFunc("GET /api/v1/app/android/apk", h.androidAppAPK)
}

func (h *Hub) androidAppManifest(w http.ResponseWriter) ([]byte, string, bool) {
	dir := h.androidUpdateDir()
	b, err := os.ReadFile(filepath.Join(dir, "latest.json"))
	if err != nil {
		androidAppError(w, err)
		return nil, "", false
	}
	return b, dir, true
}

func (h *Hub) androidAppLatest(w http.ResponseWriter, r *http.Request) {
	b, _, ok := h.androidAppManifest(w)
	if !ok {
		return
	}
	var release map[string]json.RawMessage
	if err := json.Unmarshal(b, &release); err != nil {
		writeErr(w, http.StatusInternalServerError, i18n.Err("hub-androidapp.bad_manifest"))
		return
	}
	var checksum string
	if err := json.Unmarshal(release["sha256"], &checksum); err != nil || !validAPKDigest(checksum) {
		writeErr(w, http.StatusInternalServerError, i18n.Err("hub-androidapp.bad_manifest"))
		return
	}
	// Existing clients already honor download_url, including its query string.
	release["download_url"] = json.RawMessage(strconv.Quote("/api/v1/app/android/apk?sha256=" + strings.ToLower(checksum)))
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(release); err != nil {
		h.logger.Printf("android update metadata response: %v", err)
		return
	}
}

func (h *Hub) androidAppAPK(w http.ResponseWriter, r *http.Request) {
	dir := h.androidUpdateDir()
	checksum := r.URL.Query().Get("sha256")
	if checksum != "" {
		if !validAPKDigest(checksum) {
			writeErr(w, http.StatusBadRequest, i18n.Err("hub-androidapp.bad_manifest"))
			return
		}
		h.serveAndroidAPK(w, r, dir, strings.ToLower(checksum))
		return
	}
	b, dir, ok := h.androidAppManifest(w)
	if !ok {
		return
	}
	// Content-addressed files let publication replace only the manifest atomically.
	var release struct {
		SHA256 string `json:"sha256"`
	}
	err := json.Unmarshal(b, &release)
	digest, decodeErr := hex.DecodeString(release.SHA256)
	if err != nil || decodeErr != nil || len(digest) != 32 {
		writeErr(w, http.StatusInternalServerError, i18n.Err("hub-androidapp.bad_manifest"))
		return
	}
	h.serveAndroidAPK(w, r, dir, release.SHA256)
}

func validAPKDigest(value string) bool {
	digest, err := hex.DecodeString(value)
	return err == nil && len(digest) == 32
}

func (h *Hub) serveAndroidAPK(w http.ResponseWriter, r *http.Request, dir, checksum string) {
	f, err := os.Open(filepath.Join(dir, checksum+".apk"))
	if err != nil {
		androidAppError(w, err)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		writeErr(w, http.StatusInternalServerError, i18n.Err("hub-androidapp.unreadable_apk"))
		return
	}
	w.Header().Set("Content-Type", "application/vnd.android.package-archive")
	w.Header().Set("Content-Disposition", `attachment; filename="persona.apk"`)
	if serveAndroidAPKGzip(w, r, dir, checksum) {
		return
	}
	w.Header().Set("ETag", `"`+checksum+`"`)
	http.ServeContent(w, r, "persona.apk", info.ModTime(), f)
}

// serveAndroidAPKGzip sends the gzip copy the publisher writes next to the APK (scripts/release/android_apk_atomic.py)
// to a request gzipResponses would compress (gzipWanted: it takes gzip, and is no HEAD or range). An APK keeps its dex
// files and resources.arsc uncompressed so the phone can map them, so the copy is about half the bytes. OkHttp and
// browsers decode it before anything checks the SHA-256, and gzipResponses passes an answer that has its own
// Content-Encoding on as it is. It reports false, having written nothing, when there is no copy: the request then gets
// the APK as before.
func serveAndroidAPKGzip(w http.ResponseWriter, r *http.Request, dir, checksum string) bool {
	if !gzipWanted(r) {
		return false
	}
	f, err := os.Open(filepath.Join(dir, checksum+".apk.gz"))
	if err != nil {
		return false
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	h := w.Header()
	h.Set("Content-Encoding", "gzip")
	h.Add("Vary", "Accept-Encoding")
	// Other bytes, so another validator: a conditional request carrying the plain APK's tag gets these in full.
	h.Set("ETag", `"`+checksum+`-gzip"`)
	// ServeContent leaves out the length of an encoded body (it may be compressed as it is written); this one is the
	// file, so a browser can show the download's progress.
	h.Set("Content-Length", strconv.FormatInt(info.Size(), 10))
	http.ServeContent(fileOnlyHeaders{w}, r, "persona.apk", info.ModTime(), f)
	return true
}

// fileOnlyHeaders drops the gzip copy's Content-Length and Content-Encoding from any answer other than 200: those
// carry no file. ServeContent drops them itself from a 304 and its errors, but its 412 (If-Match or
// If-Unmodified-Since failed) only writes the status, and an empty body under the file's length is one the client
// waits for in vain.
type fileOnlyHeaders struct{ http.ResponseWriter }

func (w fileOnlyHeaders) WriteHeader(code int) {
	if code != http.StatusOK {
		w.Header().Del("Content-Length")
		w.Header().Del("Content-Encoding")
	}
	w.ResponseWriter.WriteHeader(code)
}

// Unwrap lets http.ResponseController reach the writer beneath.
func (w fileOnlyHeaders) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func androidAppError(w http.ResponseWriter, err error) {
	if os.IsNotExist(err) {
		writeErrCode(w, http.StatusNotFound, "not_found", i18n.Err("hub-androidapp.not_published"))
		return
	}
	writeErr(w, http.StatusInternalServerError, i18n.Err("hub-androidapp.unreadable_release"))
}
