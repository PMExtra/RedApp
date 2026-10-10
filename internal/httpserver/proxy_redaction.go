package httpserver

import (
	"encoding/json"

	"github.com/PMExtra/RedApp/internal/networkproxy"
	"github.com/PMExtra/RedApp/internal/store"
)

// Administrative JSON never carries saved proxy passwords. Only the explicit
// configuration export with credentials enabled returns the saved URLs.
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

func redactConfiguration(c store.Configuration) store.Configuration {
	c.ProxyEffective.Config = c.ProxyEffective.Config.Redacted()
	for _, values := range []*store.Object{&c.Defaults, &c.Overrides, &c.Effective} {
		if proxy, ok := (*values)["proxy"]; ok {
			copied := store.Object{}
			for key, value := range *values {
				copied[key] = value
			}
			copied["proxy"] = redactProxyValue(proxy)
			*values = copied
		}
	}
	return c
}

func redactImportPlan(plan store.ImportPlan) store.ImportPlan {
	items := make([]store.ImportItem, len(plan.Items))
	for i, item := range plan.Items {
		differences := make([]store.ImportDifference, len(item.Differences))
		for j, difference := range item.Differences {
			if difference.Field == "proxy" {
				difference.Before = redactProxyValue(difference.Before)
				difference.After = redactProxyValue(difference.After)
			}
			differences[j] = difference
		}
		item.Differences = differences
		items[i] = item
	}
	plan.Items = items
	return plan
}
