package toopenai

import (
	"strings"
)

func convertToolsToOpenAI(toolsRaw interface{}) []map[string]interface{} {
	toolsArr, ok := toolsRaw.([]interface{})
	if !ok {
		return nil
	}
	var result []map[string]interface{}
	for _, t := range toolsArr {
		toolMap, ok := t.(map[string]interface{})
		if !ok {
			continue
		}
		if decls, ok := toolMap["functionDeclarations"].([]interface{}); ok {
			for _, d := range decls {
				decl, ok := d.(map[string]interface{})
				if !ok {
					continue
				}
				name, _ := decl["name"].(string)
				desc, _ := decl["description"].(string)
				params := decl["parameters"]
				if params == nil {
					params = map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}
				}
				result = append(result, map[string]interface{}{
					"type": "function",
					"function": map[string]interface{}{
						"name":        name,
						"description": desc,
						"parameters":  params,
					},
				})
			}
		}
	}
	return result
}

func convertToolChoiceToOpenAI(tcRaw interface{}) interface{} {
	tc, ok := tcRaw.(map[string]interface{})
	if !ok {
		return nil
	}
	fcc, ok := tc["functionCallingConfig"].(map[string]interface{})
	if !ok {
		return nil
	}
	mode, _ := fcc["mode"].(string)
	switch strings.ToUpper(mode) {
	case "NONE":
		return "none"
	case "ANY":
		allowed, _ := fcc["allowedFunctionNames"].([]interface{})
		if len(allowed) == 1 {
			name, _ := allowed[0].(string)
			return map[string]interface{}{
				"type":     "function",
				"function": map[string]interface{}{"name": name},
			}
		}
		return "required"
	default:
		return "auto"
	}
}
