package media

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"
)

const (
	svgNamespace   = "http://www.w3.org/2000/svg"
	xhtmlNamespace = "http://www.w3.org/1999/xhtml"
	mathNamespace  = "http://www.w3.org/1998/Math/MathML"
	xmlNamespace   = "http://www.w3.org/XML/1998/namespace"
)

// SVG icons are only displayed as images under a sandbox CSP, which already
// keeps them inert. Sanitizing is defense in depth with one goal: the stored
// file has no script, event handler, animation or reference outside itself.
// Everything else, including editor metadata, is kept byte for byte.
var svgActiveElements = map[string]bool{
	"script": true, "foreignObject": true, "handler": true, "listener": true,
	"animate": true, "animateColor": true, "animateMotion": true, "animateTransform": true, "set": true,
}

// SanitizeRequiredError lists the SVG content an upload removes only after the
// caller agrees to sanitizing.
type SanitizeRequiredError struct {
	Removed []string
}

func (e *SanitizeRequiredError) Error() string {
	return "SVG content must be removed: " + strings.Join(e.Removed, ", ")
}

// svgEdit replaces input[start:end] with text.
type svgEdit struct {
	start, end int64
	text       string
}

type svgElement struct {
	raw       string
	namespace string
	prefixes  map[string]string
	start     int64
	removed   bool // removed with its subtree
	style     *strings.Builder
	styleSafe bool
}

// sanitizeSVG returns input with active and external content removed and a
// summary of what was removed; output is input itself when nothing is.
func sanitizeSVG(input []byte) ([]byte, []string, error) {
	invalid := func(reason string) ([]byte, []string, error) {
		return nil, nil, fmt.Errorf("%w: SVG %s", ErrInvalidIcon, reason)
	}
	decoder := xml.NewDecoder(bytes.NewReader(input))
	var edits []svgEdit
	var removed []string
	var stack []*svgElement
	rootSeen, rootDone, nodes := false, false, 0
	for first := true; ; first = false {
		start := decoder.InputOffset()
		token, err := decoder.RawToken()
		if err == io.EOF {
			break
		}
		if err != nil {
			return invalid("is malformed XML")
		}
		var parent *svgElement
		if len(stack) > 0 {
			parent = stack[len(stack)-1]
		}
		skipped := parent != nil && parent.removed
		switch node := token.(type) {
		case xml.StartElement:
			if rootDone {
				return invalid("must have exactly one root element")
			}
			nodes++
			if nodes > 4096 || len(stack) >= 32 || len(node.Attr) > 64 {
				return nil, nil, ErrTooLarge
			}
			element, err := openSVGElement(node, parent, start)
			if err != nil {
				return nil, nil, err
			}
			stack = append(stack, element)
			if !rootSeen {
				rootSeen = true
				if node.Name.Local != "svg" || element.namespace != svgNamespace {
					return invalid("root must be an svg element in the SVG namespace")
				}
				if err := checkSVGRootSize(node); err != nil {
					return nil, nil, err
				}
			}
			if skipped {
				element.removed = true
				continue
			}
			if parent != nil && parent.style != nil {
				parent.styleSafe = false
			}
			if element.namespace == xhtmlNamespace || element.namespace == mathNamespace ||
				element.namespace == svgNamespace && svgActiveElements[node.Name.Local] {
				element.removed = true
				removed = append(removed, "<"+element.raw+">")
				continue
			}
			if element.namespace == svgNamespace && node.Name.Local == "style" {
				element.style, element.styleSafe = &strings.Builder{}, true
			}
			kept, dropped := sanitizeSVGAttributes(node)
			if len(dropped) > 0 {
				for _, attr := range dropped {
					removed = append(removed, "<"+element.raw+" "+attr+">")
				}
				// A self-closing tag is reported twice at the same offset.
				end := decoder.InputOffset()
				closing := ">"
				if bytes.HasSuffix(input[start:end], []byte("/>")) {
					closing = "/>"
				}
				edits = append(edits, svgEdit{start, end, startTag(element.raw, kept) + closing})
			}
		case xml.EndElement:
			if parent == nil || rawName(node.Name) != parent.raw {
				return invalid("has mismatched tags")
			}
			stack = stack[:len(stack)-1]
			outerRemoved := len(stack) > 0 && stack[len(stack)-1].removed
			unsafeStyle := !parent.removed && parent.style != nil && (!parent.styleSafe || unsafeCSS(parent.style.String(), true))
			if unsafeStyle {
				removed = append(removed, "<"+parent.raw+">")
			}
			if parent.removed && !outerRemoved || unsafeStyle {
				// The whole element goes, including edits made inside it.
				edits = dropNested(edits, parent.start)
				edits = append(edits, svgEdit{parent.start, decoder.InputOffset(), ""})
			}
			if len(stack) == 0 {
				rootDone = true
			}
		case xml.CharData:
			if parent == nil {
				if len(bytes.TrimSpace(node)) != 0 {
					return invalid("has text outside the root element")
				}
				continue
			}
			if parent.style != nil {
				parent.style.Write(node)
			}
		case xml.Comment:
			// Comments are inert and kept.
		case xml.ProcInst:
			if strings.EqualFold(node.Target, "xml") {
				if !first {
					return invalid("has a misplaced XML declaration")
				}
				continue
			}
			if !skipped {
				removed = append(removed, "<?"+node.Target+"?>")
				edits = append(edits, svgEdit{start, decoder.InputOffset(), ""})
			}
		case xml.Directive:
			return invalid("contains a DOCTYPE or entity declaration")
		default:
			return invalid("contains unsupported XML")
		}
	}
	if !rootSeen || !rootDone {
		return invalid("must contain a complete svg root element")
	}
	if len(edits) == 0 {
		return input, nil, nil
	}
	var output bytes.Buffer
	offset := int64(0)
	for _, edit := range edits {
		output.Write(input[offset:edit.start])
		output.WriteString(edit.text)
		offset = edit.end
	}
	output.Write(input[offset:])
	return output.Bytes(), summarize(removed), nil
}

// openSVGElement resolves the element's namespace from the declarations in
// scope; undeclared prefixes and duplicate attributes are not well-formed.
func openSVGElement(node xml.StartElement, parent *svgElement, start int64) (*svgElement, error) {
	element := &svgElement{raw: rawName(node.Name), start: start, prefixes: map[string]string{}}
	if parent != nil {
		for prefix, namespace := range parent.prefixes {
			element.prefixes[prefix] = namespace
		}
	}
	seen := map[string]bool{}
	for _, attr := range node.Attr {
		name := rawName(attr.Name)
		if seen[name] {
			return nil, fmt.Errorf("%w: SVG has a duplicate attribute", ErrInvalidIcon)
		}
		seen[name] = true
		if attr.Name.Space == "" && attr.Name.Local == "xmlns" {
			element.prefixes[""] = attr.Value
		} else if attr.Name.Space == "xmlns" {
			element.prefixes[attr.Name.Local] = attr.Value
		}
	}
	for _, attr := range node.Attr {
		if prefix := attr.Name.Space; prefix != "" && prefix != "xmlns" && prefix != "xml" && element.prefixes[prefix] == "" {
			return nil, fmt.Errorf("%w: SVG uses an undeclared namespace prefix", ErrInvalidIcon)
		}
	}
	namespace, ok := element.prefixes[node.Name.Space]
	if node.Name.Space != "" && (!ok || namespace == "") {
		return nil, fmt.Errorf("%w: SVG uses an undeclared namespace prefix", ErrInvalidIcon)
	}
	element.namespace = namespace
	return element, nil
}

// sanitizeSVGAttributes keeps attributes that cannot run code or reach
// outside the file and returns the raw names of the others.
func sanitizeSVGAttributes(node xml.StartElement) (kept []xml.Attr, dropped []string) {
	for _, attr := range node.Attr {
		local, value := attr.Name.Local, strings.TrimSpace(attr.Value)
		safe := true
		switch {
		case attr.Name.Space == "xmlns" || attr.Name.Space == "" && local == "xmlns":
		case len(local) > 2 && strings.EqualFold(local[:2], "on"):
			safe = false
		case attr.Name.Space == "xml" && local == "base":
			safe = false
		case local == "href":
			safe = strings.HasPrefix(value, "#") || node.Name.Local == "image" && embeddedRaster(value)
		case local == "style":
			safe = !unsafeCSS(value, true)
		default:
			safe = !unsafeCSS(value, false)
		}
		if safe {
			kept = append(kept, attr)
		} else {
			dropped = append(dropped, rawName(attr.Name))
		}
	}
	return kept, dropped
}

func embeddedRaster(value string) bool {
	for _, kind := range []string{"png", "jpeg", "gif", "webp"} {
		if strings.HasPrefix(strings.ToLower(value), "data:image/"+kind+";base64,") {
			return true
		}
	}
	return false
}

// unsafeCSS reports CSS that may load a resource: url() other than a local
// #id, image functions, escapes that could spell those and, in style sheets
// and style attributes, every at-rule.
func unsafeCSS(value string, styleSheet bool) bool {
	lower := strings.ToLower(value)
	if strings.Contains(lower, `\`) {
		return true
	}
	if styleSheet {
		if strings.Contains(lower, "@") {
			return true
		}
		for _, function := range []string{"image(", "image-set(", "cross-fade(", "element(", "src(", "expression("} {
			if strings.Contains(lower, function) {
				return true
			}
		}
	}
	for rest := lower; ; {
		i := strings.Index(rest, "url(")
		if i < 0 {
			return false
		}
		rest = strings.TrimLeft(rest[i+4:], " \t\r\n\f\"'")
		if !strings.HasPrefix(rest, "#") {
			return true
		}
	}
}

// checkSVGRootSize bounds the intrinsic size declared in pixels or percent.
func checkSVGRootSize(node xml.StartElement) error {
	var width, height float64
	for _, attr := range node.Attr {
		if attr.Name.Space != "" || attr.Name.Local != "width" && attr.Name.Local != "height" {
			continue
		}
		value := strings.TrimSuffix(strings.TrimSpace(attr.Value), "px")
		percent := strings.HasSuffix(value, "%")
		number, err := strconv.ParseFloat(strings.TrimSuffix(value, "%"), 64)
		if err != nil {
			continue
		}
		if percent && number > 100 || !percent && number > MaxDimension {
			return ErrTooLarge
		}
		if !percent && attr.Name.Local == "width" {
			width = number
		} else if !percent {
			height = number
		}
	}
	if width*height > MaxPixels {
		return ErrTooLarge
	}
	return nil
}

func rawName(name xml.Name) string {
	if name.Space == "" {
		return name.Local
	}
	return name.Space + ":" + name.Local
}

func startTag(name string, attrs []xml.Attr) string {
	var tag strings.Builder
	tag.WriteString("<" + name)
	for _, attr := range attrs {
		tag.WriteString(" " + rawName(attr.Name) + `="`)
		xml.EscapeText(&tag, []byte(attr.Value))
		tag.WriteString(`"`)
	}
	return tag.String()
}

// dropNested removes edits inside an element that is removed as a whole.
func dropNested(edits []svgEdit, start int64) []svgEdit {
	for len(edits) > 0 && edits[len(edits)-1].start >= start {
		edits = edits[:len(edits)-1]
	}
	return edits
}

// summarize counts repeated items and bounds the list for display.
func summarize(items []string) []string {
	const limit = 20
	counts := map[string]int{}
	var order []string
	for _, item := range items {
		if counts[item] == 0 {
			order = append(order, item)
		}
		counts[item]++
	}
	var summary []string
	for i, item := range order {
		if i == limit {
			summary = append(summary, fmt.Sprintf("… +%d", len(order)-limit))
			break
		}
		if counts[item] > 1 {
			item += fmt.Sprintf(" ×%d", counts[item])
		}
		summary = append(summary, item)
	}
	return summary
}
