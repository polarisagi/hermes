package togoogle

import (
	"strings"
)

// mapTools 将 Anthropic 的 Tools 数组映射为 Gemini 的 tools 数组
func mapTools(tools []Tool) []map[string]interface{} {
	if len(tools) == 0 {
		return nil
	}

	var functionDeclarations []map[string]interface{}
	for _, t := range tools {
		if t.Type != "" {
			if decl := builtinToolToFunctionDecl(t); decl != nil {
				functionDeclarations = append(functionDeclarations, decl)
			}
			continue
		}
		var params map[string]interface{}
		if t.InputSchema != nil {
			params = sanitizeSchema(t.InputSchema)
		}
		decl := map[string]interface{}{
			"name":        t.Name,
			"description": t.Description,
			"parameters":  params,
		}
		functionDeclarations = append(functionDeclarations, decl)
	}

	if len(functionDeclarations) > 0 {
		return []map[string]interface{}{
			{
				"functionDeclarations": functionDeclarations,
			},
		}
	}
	return nil
}

// mapToolChoice 将 Anthropic 的 ToolChoice 映射为 Gemini 的 functionCallingConfig
func mapToolChoice(toolChoice *ToolChoice) map[string]interface{} {
	if toolChoice == nil {
		return nil
	}

	mode := "AUTO"
	var allowedNames []string

	switch toolChoice.Type {
	case "none":
		mode = "NONE"
	case "any":
		mode = "ANY"
	case "tool":
		mode = "ANY"
		if toolChoice.Name != "" {
			allowedNames = []string{toolChoice.Name}
		}
	}

	funcConfig := map[string]interface{}{
		"mode": mode,
	}
	if len(allowedNames) > 0 {
		funcConfig["allowedFunctionNames"] = allowedNames
	}
	if toolChoice.DisableParallelToolUse {
		funcConfig["disableParallelFunctionCalls"] = true
	}

	return map[string]interface{}{
		"functionCallingConfig": funcConfig,
	}
}

// builtinToolToFunctionDecl 将 Anthropic 内置工具类型转换为 Gemini functionDeclaration
func builtinToolToFunctionDecl(t Tool) map[string]interface{} {
	name := t.Name
	desc := t.Description
	var params map[string]interface{}

	if t.InputSchema != nil {
		params = sanitizeSchema(t.InputSchema)
	} else {
		params = buildBuiltinSchema(t.Type, name)
	}

	return map[string]interface{}{
		"name":        name,
		"description": desc,
		"parameters":  params,
	}
}

func buildBuiltinSchema(tType, name string) map[string]interface{} {
	switch {
	case strings.Contains(tType, "bash") || strings.Contains(name, "bash"):
		return buildBashSchema()
	case strings.Contains(tType, "str_replace_based_edit_tool") || strings.Contains(name, "str_replace_based_edit_tool"),
		strings.Contains(tType, "text_editor") || strings.Contains(name, "text_editor"):
		isNewEditor := strings.Contains(tType, "20250728") || strings.Contains(name, "20250728")
		return buildTextEditorSchema(isNewEditor)
	case strings.Contains(tType, "computer") || strings.Contains(name, "computer"):
		isNewComputer := strings.Contains(tType, "20251124") || strings.Contains(name, "20251124")
		return buildComputerSchema(isNewComputer)
	case strings.Contains(tType, "web_search") || strings.Contains(name, "web_search"):
		return buildWebSearchSchema()
	case strings.Contains(tType, "web_fetch") || strings.Contains(name, "web_fetch"):
		return buildWebFetchSchema()
	case strings.Contains(tType, "code_execution") || strings.Contains(name, "code_execution"):
		return buildCodeExecutionSchema()
	case strings.Contains(tType, "memory") || strings.Contains(name, "memory"):
		return buildMemorySchema()
	default:
		return map[string]interface{}{
			"type": "OBJECT",
			"properties": map[string]interface{}{
				"input": map[string]interface{}{
					"type":        "OBJECT",
					"description": "Tool input parameters",
				},
			},
		}
	}
}

func buildBashSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "OBJECT",
		"properties": map[string]interface{}{
			"command": map[string]interface{}{"type": "STRING", "description": "The bash command to run"},
			"restart": map[string]interface{}{"type": "BOOLEAN", "description": "Restart the bash shell session, clearing environment and history"},
			"timeout": map[string]interface{}{"type": "INTEGER", "description": "Timeout in milliseconds for the command"},
		},
		"required": []interface{}{"command"},
	}
}

func buildTextEditorSchema(isNewEditor bool) map[string]interface{} {
	editorProps := map[string]interface{}{
		"command":     map[string]interface{}{"type": "STRING", "description": "The command to run: view, create, str_replace, insert, undo_edit"},
		"path":        map[string]interface{}{"type": "STRING", "description": "Absolute path to file or directory"},
		"file_text":   map[string]interface{}{"type": "STRING", "description": "Required for create command — the new file content"},
		"old_str":     map[string]interface{}{"type": "STRING", "description": "Required for str_replace command — the text to be replaced"},
		"new_str":     map[string]interface{}{"type": "STRING", "description": "Required for str_replace command — the replacement text"},
		"insert_line": map[string]interface{}{"type": "INTEGER", "description": "Required for insert command — line number after which to insert"},
	}
	if isNewEditor {
		editorProps["view_range"] = map[string]interface{}{
			"type":        "ARRAY",
			"description": "Optional [start_line, end_line] for view command",
			"items":       map[string]interface{}{"type": "INTEGER"},
		}
		editorProps["insert_text"] = map[string]interface{}{"type": "STRING", "description": "Required for insert command — the text to insert (20250728+)"}
	}
	return map[string]interface{}{
		"type":       "OBJECT",
		"properties": editorProps,
		"required":   []interface{}{"command", "path"},
	}
}

func buildComputerSchema(isNewComputer bool) map[string]interface{} {
	actionDesc := "The computer action: screenshot, key, type, mouse_move, left_click, right_click, middle_click, double_click, triple_click, left_click_drag, left_mouse_down, left_mouse_up, scroll, hold_key, wait, cursor_position"
	if isNewComputer {
		actionDesc += ", zoom"
	}
	computerProps := map[string]interface{}{
		"action":           map[string]interface{}{"type": "STRING", "description": actionDesc},
		"coordinate":       map[string]interface{}{"type": "ARRAY", "description": "[x, y] pixel coordinates for mouse actions", "items": map[string]interface{}{"type": "INTEGER"}},
		"text":             map[string]interface{}{"type": "STRING", "description": "Text to type or key sequence (e.g. 'Return', 'ctrl+c')"},
		"direction":        map[string]interface{}{"type": "STRING", "description": "Scroll direction: up, down, left, right"},
		"amount":           map[string]interface{}{"type": "INTEGER", "description": "Number of scroll clicks"},
		"start_coordinate": map[string]interface{}{"type": "ARRAY", "description": "[x, y] drag start coordinates for left_click_drag", "items": map[string]interface{}{"type": "INTEGER"}},
		"duration":         map[string]interface{}{"type": "NUMBER", "description": "Duration in seconds for hold_key or wait actions"},
	}
	if isNewComputer {
		computerProps["region"] = map[string]interface{}{"type": "ARRAY", "description": "[x, y, width, height] region for zoom action (20251124+)", "items": map[string]interface{}{"type": "INTEGER"}}
	}
	return map[string]interface{}{
		"type":       "OBJECT",
		"properties": computerProps,
		"required":   []interface{}{"action"},
	}
}

func buildWebSearchSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "OBJECT",
		"properties": map[string]interface{}{
			"query":       map[string]interface{}{"type": "STRING", "description": "The search query"},
			"explanation": map[string]interface{}{"type": "STRING", "description": "One sentence explanation as to why this tool is being used"},
		},
		"required": []interface{}{"query", "explanation"},
	}
}

func buildWebFetchSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "OBJECT",
		"properties": map[string]interface{}{
			"url":        map[string]interface{}{"type": "STRING", "description": "The URL to fetch"},
			"prompt":     map[string]interface{}{"type": "STRING", "description": "Optional prompt describing what to extract from the page"},
			"max_length": map[string]interface{}{"type": "INTEGER", "description": "Maximum number of characters to return from the response"},
			"raw":        map[string]interface{}{"type": "BOOLEAN", "description": "Return raw HTML/content instead of processed text"},
		},
		"required": []interface{}{"url"},
	}
}

func buildCodeExecutionSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "OBJECT",
		"properties": map[string]interface{}{
			"language":   map[string]interface{}{"type": "STRING", "description": "Programming language: python, javascript, typescript, bash, etc."},
			"code":       map[string]interface{}{"type": "STRING", "description": "The code to execute"},
			"session_id": map[string]interface{}{"type": "STRING", "description": "Optional session ID to reuse an existing execution environment"},
			"timeout_ms": map[string]interface{}{"type": "INTEGER", "description": "Timeout in milliseconds (default 10000)"},
		},
		"required": []interface{}{"language", "code"},
	}
}

func buildMemorySchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "OBJECT",
		"properties": map[string]interface{}{
			"command":     map[string]interface{}{"type": "STRING", "description": "The command: view, create, str_replace, insert, delete, rename"},
			"path":        map[string]interface{}{"type": "STRING", "description": "Path within the memories store (always required)"},
			"file_text":   map[string]interface{}{"type": "STRING", "description": "Required for create command — initial file content"},
			"old_str":     map[string]interface{}{"type": "STRING", "description": "Required for str_replace — text to be replaced"},
			"new_str":     map[string]interface{}{"type": "STRING", "description": "Required for str_replace — replacement text"},
			"insert_line": map[string]interface{}{"type": "INTEGER", "description": "Required for insert — line number after which to insert"},
			"insert_text": map[string]interface{}{"type": "STRING", "description": "Required for insert — text to insert"},
			"new_path":    map[string]interface{}{"type": "STRING", "description": "Required for rename — the new path/name"},
		},
		"required": []interface{}{"command", "path"},
	}
}

// sanitizeSchema 递归清洗 JSON Schema
func sanitizeSchema(schema map[string]interface{}) map[string]interface{} {
	if schema == nil {
		return nil
	}

	result := make(map[string]interface{})

	// 先拷贝所有属性实现全面透传，过滤 Gemini API 明确不支持的 JSON Schema 字段
	for k, v := range schema {
		switch k {
		case "$schema", "$ref", "$defs", "$id",
			"propertyNames", "exclusiveMinimum", "exclusiveMaximum",
			"additionalProperties",
			"unevaluatedProperties", "unevaluatedItems",
			"if", "then", "else", "not",
			"contentEncoding", "contentMediaType",
			"patternProperties":
			continue
		case "const":
			result["enum"] = []interface{}{v}
			continue
		}
		result[k] = v
	}

	sanitizeNestedSchemas(result)
	mergeAllOf(result)
	convertOneOfToAnyOf(result)
	inferMissingType(result)
	enforceVertexTypeConstraints(result)

	return result
}

func sanitizeNestedSchemas(result map[string]interface{}) {
	if props, ok := result["properties"].(map[string]interface{}); ok {
		cleanProps := make(map[string]interface{})
		for pk, pv := range props {
			if propMap, ok := pv.(map[string]interface{}); ok {
				cleanProps[pk] = sanitizeSchema(propMap)
			} else {
				cleanProps[pk] = pv
			}
		}
		result["properties"] = cleanProps
	}

	if items, ok := result["items"].(map[string]interface{}); ok {
		result["items"] = sanitizeSchema(items)
	}

	if arr, ok := result["anyOf"].([]interface{}); ok {
		cleanArr := make([]interface{}, 0, len(arr))
		for _, item := range arr {
			if itemMap, ok := item.(map[string]interface{}); ok {
				cleanArr = append(cleanArr, sanitizeSchema(itemMap))
			} else {
				cleanArr = append(cleanArr, item)
			}
		}
		result["anyOf"] = cleanArr
	}
}

func convertOneOfToAnyOf(result map[string]interface{}) {
	if oneOfArr, ok := result["oneOf"].([]interface{}); ok {
		delete(result, "oneOf")
		cleanArr := make([]interface{}, 0, len(oneOfArr))
		for _, item := range oneOfArr {
			if itemMap, ok := item.(map[string]interface{}); ok {
				cleanArr = append(cleanArr, sanitizeSchema(itemMap))
			} else {
				cleanArr = append(cleanArr, item)
			}
		}
		if existing, ok := result["anyOf"].([]interface{}); ok {
			result["anyOf"] = append(existing, cleanArr...)
		} else {
			result["anyOf"] = cleanArr
		}
	}
}

func mergeAllOf(result map[string]interface{}) {
	if allOfArr, ok := result["allOf"].([]interface{}); ok {
		delete(result, "allOf")
		for _, item := range allOfArr {
			if itemMap, ok := item.(map[string]interface{}); ok {
				cleaned := sanitizeSchema(itemMap)
				if props, ok := cleaned["properties"].(map[string]interface{}); ok {
					if existProps, ok := result["properties"].(map[string]interface{}); ok {
						for pk, pv := range props {
							existProps[pk] = pv
						}
					} else {
						result["properties"] = props
					}
				}
				if req, ok := cleaned["required"].([]interface{}); ok {
					if existReq, ok := result["required"].([]interface{}); ok {
						result["required"] = append(existReq, req...)
					} else {
						result["required"] = req
					}
				}
				if _, hasType := result["type"]; !hasType {
					if t, ok := cleaned["type"]; ok {
						result["type"] = t
					}
				}
				if _, hasDesc := result["description"]; !hasDesc {
					if d, ok := cleaned["description"]; ok {
						result["description"] = d
					}
				}
			}
		}
	}
}

func inferMissingType(result map[string]interface{}) {
	if _, hasType := result["type"]; !hasType {
		if _, hasAnyOf := result["anyOf"]; !hasAnyOf {
			if _, hasProps := result["properties"]; hasProps {
				result["type"] = "OBJECT"
			} else if _, hasItems := result["items"]; hasItems {
				result["type"] = "ARRAY"
			} else if _, hasEnum := result["enum"]; hasEnum {
				result["type"] = "STRING"
			} else {
				result["type"] = "STRING"
			}
		}
	}
}

func enforceVertexTypeConstraints(result map[string]interface{}) {
	if typeVal, ok := result["type"]; ok {
		if tStr, ok := typeVal.(string); ok {
			result["type"] = strings.ToUpper(tStr)
		} else if tArr, ok := typeVal.([]interface{}); ok {
			var types []string
			hasNull := false

			for _, item := range tArr {
				if ts, ok := item.(string); ok {
					if strings.ToLower(ts) == "null" {
						hasNull = true
					} else {
						types = append(types, strings.ToUpper(ts))
					}
				}
			}

			if hasNull {
				result["nullable"] = true
			}

			if len(types) == 1 {
				result["type"] = types[0]
			} else if len(types) > 1 {
				delete(result, "type")
				anyOfArr := make([]interface{}, 0, len(types))
				for _, t := range types {
					anyOfArr = append(anyOfArr, map[string]interface{}{"type": t})
				}
				result["anyOf"] = anyOfArr
			} else if len(types) == 0 && hasNull {
				result["type"] = "NULL"
			}
		}
	}
}
