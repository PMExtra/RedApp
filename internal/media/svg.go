package media

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
)

const svgNamespace = "http://www.w3.org/2000/svg"

var (
	svgID        = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]{0,127}$`)
	svgNumber    = regexp.MustCompile(`^[+-]?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:[eE][+-]?[0-9]+)?$`)
	svgPathData  = regexp.MustCompile(`^[MmZzLlHhVvCcSsQqTtAa0-9eE.,+\-\s]*$`)
	svgHexColor  = regexp.MustCompile(`^#(?:[0-9a-fA-F]{3}|[0-9a-fA-F]{4}|[0-9a-fA-F]{6}|[0-9a-fA-F]{8})$`)
	svgColor     = regexp.MustCompile(`^(?:[A-Za-z]{1,24}|(?:rgb|rgba|hsl|hsla)\([0-9.,%+\-\s]+\))$`)
	svgTransform = regexp.MustCompile(`^(?:(?:matrix|translate|scale|rotate|skewX|skewY)\([0-9eE.,+\-\s]+\)[,\s]*)+$`)
)

// This is deliberately a static icon subset, not an SVG sanitizer that tries
// to preserve arbitrary input. Unsupported elements and attributes are errors.
var svgElementAttrs = map[string]string{
	"svg":            "viewBox width height preserveAspectRatio version role",
	"g":              "",
	"defs":           "",
	"title":          "",
	"desc":           "",
	"path":           "d pathLength",
	"rect":           "x y width height rx ry pathLength",
	"circle":         "cx cy r pathLength",
	"ellipse":        "cx cy rx ry pathLength",
	"line":           "x1 y1 x2 y2 pathLength",
	"polyline":       "points pathLength",
	"polygon":        "points pathLength",
	"linearGradient": "x1 y1 x2 y2 gradientUnits gradientTransform spreadMethod",
	"radialGradient": "cx cy r fx fy fr gradientUnits gradientTransform spreadMethod",
	"stop":           "offset stop-color stop-opacity",
	"clipPath":       "clipPathUnits",
}

const svgCommonAttrs = "id fill fill-rule fill-opacity stroke stroke-width stroke-linecap stroke-linejoin stroke-miterlimit stroke-dasharray stroke-dashoffset stroke-opacity opacity color transform clip-path clip-rule vector-effect"

func normalizeSVG(input []byte) ([]byte, error) {
	decoder := xml.NewDecoder(bytes.NewReader(input))
	output := &boundedBuffer{limit: MaxStoredBytes}
	encoder := xml.NewEncoder(output)
	var stack []string
	ids := make(map[string]bool)
	var references []string
	rootSeen, rootDone, nodes := false, false, 0
	invalid := func(reason string) ([]byte, error) {
		return nil, fmt.Errorf("%w: SVG %s", ErrInvalidIcon, reason)
	}
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return invalid("is malformed XML")
		}
		switch node := token.(type) {
		case xml.StartElement:
			if rootDone {
				return invalid("must have exactly one root")
			}
			nodes++
			if nodes > 4096 || len(stack) >= 32 || len(node.Attr) > 64 {
				return nil, ErrTooLarge
			}
			name := node.Name.Local
			allowed, ok := svgElementAttrs[name]
			if !ok || (node.Name.Space != "" && node.Name.Space != svgNamespace) {
				return invalid("contains an unsupported element")
			}
			if len(stack) == 0 {
				if name != "svg" || rootSeen {
					return invalid("root must be svg")
				}
				rootSeen = true
			} else if name == "svg" {
				return invalid("nested svg is unsupported")
			}
			clean := xml.StartElement{Name: xml.Name{Local: name}}
			if len(stack) == 0 {
				clean.Attr = append(clean.Attr, xml.Attr{Name: xml.Name{Local: "xmlns"}, Value: svgNamespace})
			}
			attributes := make(map[string]bool)
			var rootWidth, rootHeight float64
			for _, attr := range node.Attr {
				if attr.Name.Space == "" && attr.Name.Local == "xmlns" {
					if attr.Value != svgNamespace || len(stack) != 0 || attributes["xmlns"] {
						return invalid("contains an unsupported namespace")
					}
					attributes["xmlns"] = true
					continue
				}
				key := attr.Name.Local
				if attr.Name.Space != "" || attributes[key] || (!wordIn(allowed, key) && !wordIn(svgCommonAttrs, key)) {
					return invalid("contains an unsupported attribute")
				}
				attributes[key] = true
				value := strings.TrimSpace(attr.Value)
				if !validSVGAttribute(key, value) {
					return invalid("contains an unsupported attribute value")
				}
				if len(stack) == 0 && (key == "width" || key == "height") {
					dimension, _ := svgNumeric(strings.TrimSuffix(value, "px"), true)
					if dimension > MaxDimension || strings.HasSuffix(value, "%") && dimension > 1 {
						return nil, ErrTooLarge
					}
					if !strings.HasSuffix(value, "%") {
						if key == "width" {
							rootWidth = dimension
						} else {
							rootHeight = dimension
						}
					}
				}
				if key == "id" {
					if ids[value] {
						return invalid("contains duplicate ids")
					}
					ids[value] = true
				}
				if strings.HasPrefix(value, "url(#") {
					references = append(references, strings.TrimSuffix(strings.TrimPrefix(value, "url(#"), ")"))
				}
				clean.Attr = append(clean.Attr, xml.Attr{Name: xml.Name{Local: key}, Value: value})
			}
			if rootWidth*rootHeight > MaxPixels {
				return nil, ErrTooLarge
			}
			if err := encoder.EncodeToken(clean); err != nil {
				return nil, err
			}
			stack = append(stack, name)
		case xml.EndElement:
			if len(stack) == 0 {
				return invalid("has an unmatched end tag")
			}
			if err := encoder.EncodeToken(xml.EndElement{Name: xml.Name{Local: stack[len(stack)-1]}}); err != nil {
				return nil, err
			}
			stack = stack[:len(stack)-1]
			if len(stack) == 0 {
				rootDone = true
			}
		case xml.CharData:
			if len(bytes.TrimSpace(node)) == 0 {
				continue
			}
			if len(stack) == 0 || (stack[len(stack)-1] != "title" && stack[len(stack)-1] != "desc") {
				return invalid("text is only supported in title and desc")
			}
			if err := encoder.EncodeToken(node); err != nil {
				return nil, err
			}
		case xml.Comment:
			// Comments and processing metadata are not part of the stored icon.
		case xml.ProcInst:
			if node.Target != "xml" || rootSeen {
				return invalid("processing instructions are unsupported")
			}
		case xml.Directive:
			return invalid("directives and entities are unsupported")
		default:
			return invalid("contains unsupported XML")
		}
	}
	if !rootSeen || !rootDone || len(stack) != 0 {
		return invalid("must contain a complete svg root")
	}
	for _, ref := range references {
		if !ids[ref] {
			return invalid("references an unknown local id")
		}
	}
	if err := encoder.Flush(); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func wordIn(words, value string) bool {
	return strings.Contains(" "+words+" ", " "+value+" ")
}

func validSVGAttribute(key, value string) bool {
	switch key {
	case "role":
		return value == "img"
	case "id":
		return svgID.MatchString(value)
	case "fill", "stroke", "stop-color", "color":
		return svgHexColor.MatchString(value) || svgColor.MatchString(value) || localSVGReference(value)
	case "clip-path":
		return value == "none" || localSVGReference(value)
	case "d":
		return svgPathData.MatchString(value)
	case "transform", "gradientTransform":
		return svgTransform.MatchString(value)
	case "viewBox":
		values := strings.FieldsFunc(value, func(c rune) bool { return c == ',' || c == ' ' || c == '\t' || c == '\r' || c == '\n' })
		if len(values) != 4 {
			return false
		}
		for i, v := range values {
			number, ok := svgNumeric(v, false)
			if !ok || i >= 2 && number <= 0 {
				return false
			}
		}
		return true
	case "points":
		return svgNumberList(value)
	case "stroke-dasharray":
		return value == "none" || svgNumberList(value)
	case "fill-rule", "clip-rule":
		return value == "nonzero" || value == "evenodd"
	case "stroke-linecap":
		return wordIn("butt round square", value)
	case "stroke-linejoin":
		return wordIn("miter round bevel", value)
	case "gradientUnits", "clipPathUnits":
		return wordIn("userSpaceOnUse objectBoundingBox", value)
	case "spreadMethod":
		return wordIn("pad reflect repeat", value)
	case "vector-effect":
		return wordIn("none non-scaling-stroke", value)
	case "preserveAspectRatio":
		parts := strings.Fields(value)
		return len(parts) == 1 && parts[0] == "none" || len(parts) >= 1 && len(parts) <= 2 && wordIn("xMinYMin xMidYMin xMaxYMin xMinYMid xMidYMid xMaxYMid xMinYMax xMidYMax xMaxYMax", parts[0]) && (len(parts) == 1 || wordIn("meet slice", parts[1]))
	case "version":
		return value == "1.0" || value == "1.1" || value == "2.0"
	case "opacity", "fill-opacity", "stroke-opacity", "stop-opacity", "offset":
		number, ok := svgNumeric(value, true)
		return ok && number >= 0 && number <= 1
	default:
		number, ok := svgNumeric(strings.TrimSuffix(value, "px"), true)
		if !ok {
			return false
		}
		switch key {
		case "width", "height", "r", "rx", "ry", "fr", "pathLength", "stroke-width", "stroke-miterlimit":
			return number >= 0
		}
		return true
	}
}

func svgNumberList(value string) bool {
	values := strings.FieldsFunc(value, func(c rune) bool { return c == ',' || c == ' ' || c == '\t' || c == '\r' || c == '\n' })
	if len(values) == 0 {
		return false
	}
	for _, v := range values {
		if _, ok := svgNumeric(v, false); !ok {
			return false
		}
	}
	return true
}

func svgNumeric(value string, percent bool) (float64, bool) {
	isPercent := percent && strings.HasSuffix(value, "%")
	if isPercent {
		value = strings.TrimSuffix(value, "%")
	}
	if !svgNumber.MatchString(value) {
		return 0, false
	}
	number, err := strconv.ParseFloat(value, 64)
	if isPercent {
		number /= 100
	}
	return number, err == nil
}

func localSVGReference(value string) bool {
	return strings.HasPrefix(value, "url(#") && strings.HasSuffix(value, ")") && svgID.MatchString(value[5:len(value)-1])
}
