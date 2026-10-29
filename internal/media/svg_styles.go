package media

import (
	"encoding/xml"
	"fmt"
	"regexp"
	"strings"
)

// Exporters commonly share paint through simple class rules. Expand only this
// bounded grammar into presentation attributes; never retain executable CSS.
var svgClassName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]{0,127}$`)

type svgStyleRule struct {
	class string
	attrs []xml.Attr
}

const svgStyleProperties = "fill fill-rule fill-opacity stroke stroke-width stroke-linecap stroke-linejoin stroke-miterlimit stroke-dasharray stroke-dashoffset stroke-opacity opacity color stop-color stop-opacity clip-path clip-rule"

func parseSVGStyles(css string) ([]svgStyleRule, error) {
	var rules []svgStyleRule
	for strings.TrimSpace(css) != "" {
		selector, rest, ok := strings.Cut(css, "{")
		if !ok {
			return nil, ErrInvalidIcon
		}
		body, tail, ok := strings.Cut(rest, "}")
		if !ok {
			return nil, ErrInvalidIcon
		}
		selector = strings.TrimSpace(selector)
		if !strings.HasPrefix(selector, ".") || !svgClassName.MatchString(selector[1:]) {
			return nil, ErrInvalidIcon
		}
		rule := svgStyleRule{class: selector[1:]}
		for _, declaration := range strings.Split(body, ";") {
			if strings.TrimSpace(declaration) == "" {
				continue
			}
			key, value, ok := strings.Cut(declaration, ":")
			key, value = strings.TrimSpace(key), strings.TrimSpace(value)
			if !ok || !svgClassName.MatchString(key) || !wordIn(svgStyleProperties, key) || !validSVGAttribute(key, value) {
				return nil, ErrInvalidIcon
			}
			rule.attrs = append(rule.attrs, xml.Attr{Name: xml.Name{Local: key}, Value: value})
			if len(rule.attrs) > 64 {
				return nil, ErrTooLarge
			}
		}
		rules = append(rules, rule)
		if len(rules) > 256 {
			return nil, ErrTooLarge
		}
		css = tail
	}
	return rules, nil
}

func expandSVGClasses(node xml.StartElement, rules []svgStyleRule) (xml.StartElement, error) {
	classes := map[string]bool{}
	attrs := make([]xml.Attr, 0, len(node.Attr))
	positions := map[string]int{}
	for _, attr := range node.Attr {
		if attr.Name.Local == "class" {
			for _, class := range strings.Fields(attr.Value) {
				classes[class] = false
			}
		} else {
			positions[attr.Name.Local] = len(attrs)
			attrs = append(attrs, attr)
		}
	}
	// Equal-specificity class rules override presentation attributes in source
	// order, independent of the order of names in the class attribute.
	for _, rule := range rules {
		if _, ok := classes[rule.class]; !ok {
			continue
		}
		classes[rule.class] = true
		for _, attr := range rule.attrs {
			if !wordIn(svgElementAttrs[node.Name.Local], attr.Name.Local) && !wordIn(svgCommonAttrs, attr.Name.Local) {
				return node, fmt.Errorf("%w: SVG style property is unsupported on element", ErrInvalidIcon)
			}
			if i, ok := positions[attr.Name.Local]; ok {
				attrs[i] = attr
			} else {
				positions[attr.Name.Local] = len(attrs)
				attrs = append(attrs, attr)
			}
		}
	}
	for _, found := range classes {
		if !found {
			return node, fmt.Errorf("%w: SVG references an unknown class", ErrInvalidIcon)
		}
	}
	node.Attr = attrs
	return node, nil
}
