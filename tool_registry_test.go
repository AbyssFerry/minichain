package main

import (
	"strings"
	"testing"
)

// sampleNestedOptions 用于测试嵌套参数结构的 schema 生成。
type sampleNestedOptions struct {
	// Limit 是查询返回条数。
	Limit int `json:"limit" tool:"desc=返回条数;default=10"`
	// Tags 是可选标签列表。
	Tags []string `json:"tags,omitempty" tool:"desc=标签列表"`
}

// sampleToolArgs 用于测试带嵌套结构的工具入参生成。
type sampleToolArgs struct {
	// Query 是搜索关键词。
	Query string `json:"query" tool:"desc=搜索关键词;required"`
	// Options 是可选的查询选项。
	Options sampleNestedOptions `json:"options,omitempty" tool:"desc=查询选项"`
}

// sampleToolHandler 用于验证注册器对强类型参数的调用包装。
func sampleToolHandler(args sampleToolArgs) (string, error) {
	return args.Query + ":" + args.Options.Tags[0], nil
}

// TestToolRegistry_BuildsNestedSchema 验证注册器可从嵌套结构和 tag 生成 JSON Schema。
func TestToolRegistry_BuildsNestedSchema(t *testing.T) {
	registry := newToolRegistry()
	if err := registry.RegisterFromHandler("sample_tool", "示例工具", sampleToolHandler); err != nil {
		t.Fatalf("RegisterFromHandler failed: %v", err)
	}

	definitions := registry.Definitions()
	if len(definitions) != 1 {
		t.Fatalf("unexpected definitions length: %d", len(definitions))
	}

	definition := definitions[0]
	if definition.Function.Name != "sample_tool" {
		t.Fatalf("unexpected tool name: %s", definition.Function.Name)
	}
	if definition.Function.Description != "示例工具" {
		t.Fatalf("unexpected tool description: %s", definition.Function.Description)
	}

	parameters := definition.Function.Parameters
	if parameters["type"] != "object" {
		t.Fatalf("unexpected top-level type: %#v", parameters["type"])
	}

	properties, ok := parameters["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties missing or invalid: %#v", parameters["properties"])
	}

	querySchema, ok := properties["query"].(map[string]any)
	if !ok {
		t.Fatalf("query schema missing: %#v", properties["query"])
	}
	if querySchema["description"] != "搜索关键词" {
		t.Fatalf("unexpected query description: %#v", querySchema["description"])
	}

	optionsSchema, ok := properties["options"].(map[string]any)
	if !ok {
		t.Fatalf("options schema missing: %#v", properties["options"])
	}
	if optionsSchema["description"] != "查询选项" {
		t.Fatalf("unexpected options description: %#v", optionsSchema["description"])
	}

	optionsProperties, ok := optionsSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("nested properties missing: %#v", optionsSchema["properties"])
	}
	if _, ok := optionsProperties["limit"]; !ok {
		t.Fatalf("nested limit field missing: %#v", optionsProperties)
	}

	required, ok := parameters["required"].([]string)
	if !ok {
		t.Fatalf("required list missing or invalid: %#v", parameters["required"])
	}
	if len(required) != 1 || required[0] != "query" {
		t.Fatalf("unexpected required list: %#v", required)
	}

	nestedRequired, ok := optionsSchema["required"].([]string)
	if !ok {
		t.Fatalf("nested required list missing or invalid: %#v", optionsSchema["required"])
	}
	if len(nestedRequired) != 1 || nestedRequired[0] != "limit" {
		t.Fatalf("unexpected nested required list: %#v", nestedRequired)
	}
}

// TestToolRegistry_ExecutesTypedHandler 验证注册器包装后的执行器可以正确解码参数并调用函数。
func TestToolRegistry_ExecutesTypedHandler(t *testing.T) {
	registry := newToolRegistry()
	if err := registry.RegisterFromHandler("sample_tool", "示例工具", sampleToolHandler); err != nil {
		t.Fatalf("RegisterFromHandler failed: %v", err)
	}

	mapper := registry.Mapper()
	tool, ok := mapper["sample_tool"]
	if !ok {
		t.Fatal("tool not found in mapper")
	}

	output, err := tool(map[string]any{
		"query": "天气",
		"options": map[string]any{
			"limit": 3,
			"tags":  []any{"now"},
		},
	})
	if err != nil {
		t.Fatalf("tool execution failed: %v", err)
	}
	if output != "天气:now" {
		t.Fatalf("unexpected output: %s", output)
	}
}

// TestDefaultToolsUseGeneratedSchema 验证默认工具定义已经来自注册器生成的 schema。
func TestDefaultToolsUseGeneratedSchema(t *testing.T) {
	tools := defaultTools()
	if len(tools) != 2 {
		t.Fatalf("unexpected tool count: %d", len(tools))
	}

	var weather ToolDefinition
	for _, tool := range tools {
		if tool.Function.Name == "get_current_weather" {
			weather = tool
			break
		}
	}
	if weather.Function.Name == "" {
		t.Fatal("get_current_weather not found")
	}

	properties, ok := weather.Function.Parameters["properties"].(map[string]any)
	if !ok {
		t.Fatalf("weather properties missing: %#v", weather.Function.Parameters["properties"])
	}
	location, ok := properties["location"].(map[string]any)
	if !ok {
		t.Fatalf("location schema missing: %#v", properties["location"])
	}
	if !strings.Contains(location["description"].(string), "城市或县区") {
		t.Fatalf("unexpected location description: %#v", location["description"])
	}
}
