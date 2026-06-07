package toanthropic

import "strings"

func convertToolsToAnthropic(toolsRaw interface{}) []map[string]interface{} {
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
		// functionDeclarations 数组
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
					"name":         name,
					"description":  desc,
					"input_schema": params,
				})
			}
		}
		// 内置工具（codeExecution, googleSearch 等）跳过，Anthropic 不支持
	}
	return result
}

func convertToolChoiceToAnthropic(tcRaw interface{}) map[string]interface{} {
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
		return nil // Anthropic 无 none 概念，忽略工具调用用空 tools 实现
	case "ANY":
		allowed, _ := fcc["allowedFunctionNames"].([]interface{})
		if len(allowed) == 1 {
			name, _ := allowed[0].(string)
			return map[string]interface{}{"type": "tool", "name": name}
		}
		return map[string]interface{}{"type": "any"}
	default: // AUTO
		return map[string]interface{}{"type": "auto"}
	}
}
