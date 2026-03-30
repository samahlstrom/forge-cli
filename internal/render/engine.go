package render

import (
	"fmt"
	"regexp"
	"strings"
)

// Ctx is a template context (nested map).
type Ctx map[string]any

// Render processes a Handlebars-compatible template with the given context.
// Supports: {{var}}, {{nested.var}}, {{#if}}, {{else}}, {{/if}}, {{#unless}}, {{#each}}.
func Render(template string, ctx Ctx) string {
	result := template

	// Process {{#each items}}...{{/each}}
	eachRe := regexp.MustCompile(`(?s)[ \t]*\{\{#each\s+(\S+?)\}\}\n?(.*?)[ \t]*\{\{/each\}\}\n?`)
	result = eachRe.ReplaceAllStringFunc(result, func(match string) string {
		parts := eachRe.FindStringSubmatch(match)
		key, body := parts[1], parts[2]
		items := resolve(key, ctx)
		arr, ok := toSlice(items)
		if !ok {
			return ""
		}
		var sb strings.Builder
		for i, item := range arr {
			itemCtx := Ctx{}
			// Copy parent context
			for k, v := range ctx {
				itemCtx[k] = v
			}
			itemCtx["."] = item
			itemCtx["@index"] = i
			itemCtx["@first"] = i == 0
			itemCtx["@last"] = i == len(arr)-1
			if m, ok := item.(map[string]any); ok {
				for k, v := range m {
					itemCtx[k] = v
				}
			} else {
				itemCtx["this"] = item
			}
			sb.WriteString(Render(body, itemCtx))
		}
		return sb.String()
	})

	// Process {{#if cond}}...{{else}}...{{/if}}
	ifRe := regexp.MustCompile(`(?s)[ \t]*\{\{#if\s+(\S+?)\}\}\n?(.*?)[ \t]*\{\{/if\}\}\n?`)
	result = ifRe.ReplaceAllStringFunc(result, func(match string) string {
		parts := ifRe.FindStringSubmatch(match)
		key, body := parts[1], parts[2]
		elseBlocks := regexp.MustCompile(`(?s)[ \t]*\{\{else\}\}\n?`).Split(body, 2)
		val := resolve(key, ctx)
		if isTruthy(val) {
			return Render(elseBlocks[0], ctx)
		}
		if len(elseBlocks) > 1 {
			return Render(elseBlocks[1], ctx)
		}
		return ""
	})

	// Process {{#unless cond}}...{{/unless}}
	unlessRe := regexp.MustCompile(`(?s)[ \t]*\{\{#unless\s+(\S+?)\}\}\n?(.*?)[ \t]*\{\{/unless\}\}\n?`)
	result = unlessRe.ReplaceAllStringFunc(result, func(match string) string {
		parts := unlessRe.FindStringSubmatch(match)
		key, body := parts[1], parts[2]
		val := resolve(key, ctx)
		if !isTruthy(val) {
			return Render(body, ctx)
		}
		return ""
	})

	// Process {{variable}} substitutions
	varRe := regexp.MustCompile(`\{\{(\S+?)\}\}`)
	result = varRe.ReplaceAllStringFunc(result, func(match string) string {
		parts := varRe.FindStringSubmatch(match)
		key := parts[1]
		val := resolve(key, ctx)
		if val == nil {
			return ""
		}
		return fmt.Sprintf("%v", val)
	})

	return result
}

func resolve(path string, ctx Ctx) any {
	keys := strings.Split(path, ".")
	var current any = map[string]any(ctx)
	for _, key := range keys {
		m, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = m[key]
	}
	return current
}

func isTruthy(val any) bool {
	if val == nil {
		return false
	}
	switch v := val.(type) {
	case bool:
		return v
	case string:
		return v != ""
	case int:
		return v != 0
	case float64:
		return v != 0
	case []any:
		return len(v) > 0
	case []string:
		return len(v) > 0
	}
	return true
}

func toSlice(v any) ([]any, bool) {
	if v == nil {
		return nil, false
	}
	switch s := v.(type) {
	case []any:
		return s, true
	case []string:
		result := make([]any, len(s))
		for i, item := range s {
			result[i] = item
		}
		return result, true
	}
	return nil, false
}
