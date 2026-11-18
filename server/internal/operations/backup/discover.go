package backup

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/nwverse/persona/server/internal/identity"
	"github.com/nwverse/persona/server/internal/managerconf"
)

type hubFile struct {
	DatabaseURL string `json:"database_url"`
	Assistant   struct {
		External bool `json:"external"`
	} `json:"assistant"`
}

// Discover resolves only the hub and its built-in manager. It never enumerates
// managers/m-*: independently hosted managers are deliberately out of scope.
func Discover(opts Options) (Target, Target, error) {
	home, err := filepath.Abs(opts.Home)
	if err != nil {
		return Target{}, Target{}, fmt.Errorf("resolve home: %w", err)
	}
	var hf hubFile
	hubErr := readJSONConfig(filepath.Join(home, "hub.json"), &hf)
	if errors.Is(hubErr, os.ErrNotExist) && opts.BundleHome != "" {
		hubErr = readJSONConfig(filepath.Join(opts.BundleHome, "runtime", "hub.json"), &hf)
	}
	if hubErr != nil && !errors.Is(hubErr, os.ErrNotExist) {
		return Target{}, Target{}, fmt.Errorf("read hub.json: %w", hubErr)
	}
	if errors.Is(hubErr, os.ErrNotExist) && opts.DatabaseURL == "" && identity.Getenv("PERSONA_DATABASE_URL") == "" {
		return Target{}, Target{}, errors.New("hub.json is missing from both the runtime and bundle; provide --database-url (and --manager-database-url when it is on another server)")
	}
	if hf.Assistant.External {
		return Target{}, Target{}, errors.New("an externally hosted built-in manager is outside this backup's scope")
	}

	hubURL := opts.DatabaseURL
	if hubURL == "" {
		hubURL = identity.Getenv("PERSONA_DATABASE_URL")
	}
	if hubURL == "" {
		hubURL = hf.DatabaseURL
	}
	hub, err := makeTarget("hub", hubURL, opts.HubDockerContainer, "persona-postgres")
	if err != nil {
		return Target{}, Target{}, err
	}

	var own managerconf.OwnDatabase
	managerErr := readJSONConfig(managerconf.DefaultFile(home), &own)
	if errors.Is(managerErr, os.ErrNotExist) && opts.BundleHome != "" {
		managerErr = readJSONConfig(filepath.Join(opts.BundleHome, "runtime", "managers", "assistant", "manager.json"), &own)
	}
	if managerErr != nil && !errors.Is(managerErr, os.ErrNotExist) {
		return Target{}, Target{}, fmt.Errorf("read the built-in manager configuration: %w", managerErr)
	}

	managerURL := opts.ManagerDatabaseURL
	if managerURL == "" {
		managerURL = own.URL
	}
	managerName := own.Name
	if managerName == "" && opts.ManagerDatabaseURL == "" && own.URL == "" {
		managerName = hub.Database + "_manager"
		if own.Name == "" && managerErr != nil && opts.Manifest != nil {
			managerName = opts.Manifest.ManagerDatabase
		}
	}
	if managerURL == "" {
		if managerName == "" {
			return Target{}, Target{}, errors.New("the built-in manager database is not configured; provide --manager-database-url")
		}
		managerURL, err = withDatabase(hub.URL, managerName)
		if err != nil {
			return Target{}, Target{}, errors.New("derive the built-in manager database: invalid hub database URL")
		}
	}
	manager, err := makeTarget("manager", managerURL, opts.ManagerDockerContainer, own.Container)
	if err != nil {
		return Target{}, Target{}, err
	}
	if managerName != "" && manager.Database != managerName && opts.ManagerDatabaseURL == "" && own.URL == "" {
		return Target{}, Target{}, errors.New("the built-in manager database name does not match its database URL")
	}
	if sameDatabase(hub, manager) {
		return Target{}, Target{}, errors.New("the hub and built-in manager must have separate databases; a legacy shared database is not a whole-install backup")
	}
	return hub, manager, nil
}

func readJSONConfig(name string, output any) error {
	raw, err := os.ReadFile(name)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, output)
}

func makeTarget(part, rawURL, container, defaultContainer string) (Target, error) {
	if rawURL == "" {
		return Target{}, fmt.Errorf("%s database URL is not configured", part)
	}
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
		return Target{}, fmt.Errorf("%s database URL is not a postgres:// URL", part)
	}
	cfg, err := parseAddressing(rawURL)
	if err != nil || cfg.Database == "" {
		return Target{}, fmt.Errorf("%s database URL must name a database", part)
	}
	// The connection goes where pgx says (host= in the query overrides the URL
	// authority, and a list means any of them), so that decides on the local
	// container too.
	hosts := []string{cfg.Host}
	for _, fallback := range cfg.Fallbacks {
		hosts = append(hosts, fallback.Host)
	}
	for _, host := range hosts {
		if !loopbackDatabaseHost(host) {
			defaultContainer = ""
		}
	}
	return Target{Part: part, URL: rawURL, Database: cfg.Database, Container: container, DefaultContainer: defaultContainer}, nil
}

func loopbackDatabaseHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func withDatabase(rawURL, name string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || name == "" {
		return "", errors.New("invalid database URL or name")
	}
	u.Path, u.RawPath = "/"+name, ""
	return u.String(), nil
}

// sameDatabase reports whether a and b may be one database: the same name on
// any server both can reach. pgx tries every host of a multi-host URL in turn,
// so one host:port in common (in any position) is enough.
func sameDatabase(a, b Target) bool {
	if a.Database != b.Database {
		return false
	}
	ac, aerr := parseAddressing(a.URL)
	bc, berr := parseAddressing(b.URL)
	if aerr != nil || berr != nil {
		return false
	}
	servers := map[string]bool{}
	for _, server := range databaseServers(ac) {
		servers[server] = true
	}
	for _, server := range databaseServers(bc) {
		if servers[server] {
			return true
		}
	}
	return false
}

// databaseServers is every host:port pgx would try, in its order.
func databaseServers(cfg *pgx.ConnConfig) []string {
	key := func(host string, port uint16) string {
		if !strings.HasPrefix(host, "/") { // a host name, not a socket directory
			host = strings.ToLower(host)
		}
		return host + "\x00" + strconv.Itoa(int(port))
	}
	servers := []string{key(cfg.Host, cfg.Port)}
	for _, fallback := range cfg.Fallbacks {
		servers = append(servers, key(fallback.Host, fallback.Port))
	}
	return servers
}

// knowledgeBaseLocator resolves kb_dir without ever consulting another
// manager's configuration. A relative kb_dir is relative to managers/assistant.
func knowledgeBaseLocator(home string) (KnowledgeBaseLocator, string, error) {
	kb := managerconf.KBDir(home)
	homeAbs, err := filepath.Abs(home)
	if err != nil {
		return KnowledgeBaseLocator{}, "", err
	}
	kbAbs, err := filepath.Abs(kb)
	if err != nil {
		return KnowledgeBaseLocator{}, "", err
	}
	rel, err := filepath.Rel(homeAbs, kbAbs)
	if err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return KnowledgeBaseLocator{Relative: filepath.ToSlash(rel)}, kbAbs, nil
	}
	return KnowledgeBaseLocator{External: kbAbs}, kbAbs, nil
}
