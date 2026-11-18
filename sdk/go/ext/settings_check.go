package ext

import (
	"net/http"
	"net/url"

	"github.com/nwverse/persona/sdk/go/wire"
)

// SettingsCheckClient is the HTTP client a settings.check reaches the settings it checks with (#8459). With
// params.Proxy, it goes through the hub's proxy for this check only, which lets it reach the addresses the proposed
// settings name before they are saved. Without one (an older hub, or a program the proxy does not hold) it is an
// ordinary client, through the proxy of the program's environment. Use it only while the check runs: the hub revokes
// the check's proxy once the check answers.
func SettingsCheckClient(params wire.SettingsCheckParams) *http.Client {
	if params.Proxy == "" {
		return &http.Client{}
	}
	u, err := url.Parse(params.Proxy)
	if err != nil || u.Host == "" {
		return &http.Client{}
	}
	t, ok := http.DefaultTransport.(*http.Transport)
	if ok {
		t = t.Clone()
	} else {
		t = &http.Transport{}
	}
	t.Proxy = http.ProxyURL(u)
	t.DisableKeepAlives = true // the credential ends with the check: no connection is kept for later
	return &http.Client{Transport: t}
}
