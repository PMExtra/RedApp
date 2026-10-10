package httpserver

import (
	"encoding/json"

	"github.com/PMExtra/RedApp/internal/networkproxy"
)

// Administrative JSON never carries saved proxy passwords: they are shown as
// "****" (networkproxy.RedactedPassword). Only the configuration export with
// include_proxy_credentials returns the saved URLs.

// redactProxyValue redacts the url of a ProxyConfig-shaped JSON value.
func redactProxyValue(value any) any {
	var proxy map[string]any
	raw, _ := json.Marshal(value)
	if json.Unmarshal(raw, &proxy) != nil || proxy == nil {
		return value
	}
	if u, ok := proxy["url"].(string); ok {
		proxy["url"] = networkproxy.RedactURL(u)
	}
	return proxy
}
