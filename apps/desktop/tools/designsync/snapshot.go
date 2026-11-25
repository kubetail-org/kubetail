package main

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"math"
	"slices"
	"strings"
)

// What the hash covers: what a layer looks like and how it lays out. A
// property left out, such as the description, can change without the
// component reading as edited.
var hashed = []string{
	"type", "name", "visible", "opacity", "blendMode", "isMask", "rotation", "size",
	"layoutMode", "layoutWrap", "primaryAxisSizingMode", "counterAxisSizingMode", "primaryAxisAlignItems",
	"counterAxisAlignItems", "counterAxisAlignContent", "itemSpacing", "counterAxisSpacing",
	"paddingLeft", "paddingRight", "paddingTop", "paddingBottom", "layoutSizingHorizontal", "layoutSizingVertical",
	"layoutPositioning", "minWidth", "maxWidth", "minHeight", "maxHeight", "constraints", "clipsContent",
	"fills", "strokes", "strokeWeight", "individualStrokeWeights", "strokeAlign", "strokeCap", "strokeJoin",
	"strokeDashes", "effects", "cornerRadius", "rectangleCornerRadii", "arcData", "styles", "boundVariables",
	"characters", "style", "characterStyleOverrides", "styleOverrideTable",
	"componentProperties", "componentPropertyDefinitions", "explicitVariableModes",
}

// What a screen's hash covers of an instance: which component it is, where it
// sits and how the screen sets it. What the component draws is its own hash's.
var hashedInstance = []string{
	"type", "name", "visible", "opacity", "rotation", "size", "layoutSizingHorizontal", "layoutSizingVertical",
	"layoutPositioning", "constraints", "componentProperties", "explicitVariableModes",
}

// snapshot returns what the hash covers of n and the layers under it. A
// component's snapshot holds every layer, its instances' included. A screen's
// stops at each instance, and leaves out its stamp and the sections nested in
// it, which are screens of their own.
func (p *page) snapshot(n node, screen bool) map[string]any {
	if screen && n["type"] == "INSTANCE" {
		out := pick(n, hashedInstance)
		out["relativeTransform"] = n["relativeTransform"]
		out["main"] = p.mainKey(n)
		out["overrides"] = p.overrides(n)
		return out
	}

	out := pick(n, hashed)
	out["relativeTransform"] = n["relativeTransform"]
	switch n["type"] {
	case "INSTANCE":
		out["main"] = p.mainKey(n)
	case "VECTOR", "BOOLEAN_OPERATION", "STAR", "LINE", "REGULAR_POLYGON":
		// No other property gives these layers' outline.
		out["fillGeometry"] = n["fillGeometry"]
		out["strokeGeometry"] = n["strokeGeometry"]
	}
	if kids, ok := n["children"].([]any); ok {
		list := []any{}
		for _, k := range kids {
			c := k.(node)
			if screen && (c["type"] == "SECTION" || isStamp(c)) {
				continue
			}
			list = append(list, p.snapshot(c, screen))
		}
		out["children"] = list
	}
	return out
}

// overrides returns every value a screen overrides inside an instance, by
// layer. The API names each overridden field; the value is the layer's own.
func (p *page) overrides(instance node) []any {
	layers := map[string]node{}
	for _, n := range descendants(instance) {
		layers[id(n)] = n
	}
	var out []any
	list, _ := instance["overrides"].([]any)
	for _, item := range list {
		o := item.(node)
		layer := layers[id(o)]
		v := map[string]any{"id": id(o)}
		fields, _ := o["overriddenFields"].([]any)
		for _, f := range fields {
			field := f.(string)
			if value, ok := overridden(layer, field); ok {
				v[field] = value
			}
		}
		// A swapped instance: its fields name the swap, not the component.
		if layer["type"] == "INSTANCE" {
			v["main"] = p.mainKey(layer)
		}
		out = append(out, v)
	}
	slices.SortFunc(out, func(a, b any) int { return strings.Compare(id(a.(node)), id(b.(node))) })
	return out
}

// overridden finds an overridden field's value on its layer. The API names
// fields as the plugin API does, which keeps some under other names.
func overridden(layer node, field string) (any, bool) {
	switch field {
	case "width", "height":
		field = "size"
	case "text":
		field = "characters"
	case "inheritTextStyleId":
		field = "styles"
	}
	if v, ok := layer[field]; ok {
		return v, true
	}
	style, _ := layer["style"].(map[string]any)
	v, ok := style[field]
	return v, ok
}

// mainKey is the key of the component an instance draws, or nil when the page
// does not list it.
func (p *page) mainKey(instance node) any {
	componentID, _ := instance["componentId"].(string)
	if meta, ok := p.Components[componentID]; ok {
		return meta.Key
	}
	return nil
}

func pick(n node, names []string) map[string]any {
	out := map[string]any{}
	for _, name := range names {
		if v, ok := n[name]; ok {
			out[name] = v
		}
	}
	return out
}

// hash is 64-bit FNV-1a over the snapshot's JSON, with numbers rounded to two
// decimals to keep float noise out. The hash only has to tell an edit from none.
func hash(snap map[string]any) string {
	data, err := json.Marshal(rounded(snap))
	if err != nil { // coverage:ignore a snapshot holds only what JSON decoded
		panic(err)
	}
	h := fnv.New64a()
	h.Write(data)
	return fmt.Sprintf("%016x", h.Sum64())
}

func rounded(v any) any {
	switch v := v.(type) {
	case float64:
		return math.Round(v*100) / 100
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, x := range v {
			out[k] = rounded(x)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, x := range v {
			out[i] = rounded(x)
		}
		return out
	}
	return v
}
