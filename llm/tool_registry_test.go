package llm
import "testing"

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
	// Options 是可选查询选项。
	Options sampleNestedOptions `json:"options,omitempty" tool:"desc=查询选项"`
}

// sampleToolHandler 用于验证注册器对强类型参数的包装执行。
func sampleToolHandler(args sampleToolArgs) (string, error) {
	return args.Query + ":" + args.Options.Tags[0], nil
}

// TestToolRegistry_BuildsNestedSchema 验证函数注册可自动生成嵌套 schema。
func TestToolRegistry_BuildsNestedSchema(t *testing.T) {
	registry := NewToolRegistry()
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
	if querySchema["type"] != "string" {
		t.Fatalf("unexpected query type: %#v", querySchema["type"])
	}
	if querySchema["description"] != "搜索关键词" {
		t.Fatalf("unexpected query description: %#v", querySchema["description"])
	}

	optionsSchema, ok := properties["options"].(map[string]any)
	if !ok {
		t.Fatalf("options schema missing: %#v", properties["options"])
	}
	optionsProperties, ok := optionsSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("options properties missing: %#v", optionsSchema["properties"])
	}
	limitSchema, ok := optionsProperties["limit"].(map[string]any)
	if !ok {
		t.Fatalf("limit schema missing: %#v", optionsProperties["limit"])
	}
	if limitSchema["default"] != "10" {
		t.Fatalf("unexpected limit default: %#v", limitSchema["default"])
	}

	requiredList, ok := parameters["required"].([]string)
	if !ok {
		t.Fatalf("required list missing: %#v", parameters["required"])
	}
	if len(requiredList) != 1 || requiredList[0] != "query" {
		t.Fatalf("unexpected required fields: %#v", requiredList)
	}
}

// TestToolRegistry_ExecutesTypedHandler 验证执行器可正确解码并执行。
func TestToolRegistry_ExecutesTypedHandler(t *testing.T) {
	registry := NewToolRegistry()
	if err := registry.RegisterFromHandler("sample_tool", "示例工具", sampleToolHandler); err != nil {
		t.Fatalf("RegisterFromHandler failed: %v", err)
	}

	tool, ok := registry.Mapper()["sample_tool"]
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

// TestToolRegistry_RegisterSpec 验证显式 ToolSpec 注册方式。
func TestToolRegistry_RegisterSpec(t *testing.T) {
	registry := NewToolRegistry()
	err := registry.RegisterSpec(ToolSpec{
		Name:        "echo",
		Description: "回显输入",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"text": map[string]any{"type": "string"},
			},
			"required": []string{"text"},
		},
		Executor: func(arguments map[string]any) (string, error) {
			text, _ := arguments["text"].(string)
			return text, nil
		},
	})
	if err != nil {
		t.Fatalf("RegisterSpec failed: %v", err)
	}

	tool := registry.Mapper()["echo"]
	result, err := tool(map[string]any{"text": "hello"})
	if err != nil {
		t.Fatalf("executor failed: %v", err)
	}
	if result != "hello" {
		t.Fatalf("unexpected result: %s", result)
	}
}

// noArgumentToolHandler 用于验证必须声明结构体参数。
func noArgumentToolHandler() (string, error) {
	return "", nil
}

// stringArgumentToolHandler 用于验证参数必须是结构体类型。
func stringArgumentToolHandler(input string) (string, error) {
	return input, nil
}

// TestToolRegistry_RegisterFromHandlerRejectsNoArgument 验证无参数函数注册失败。
func TestToolRegistry_RegisterFromHandlerRejectsNoArgument(t *testing.T) {
	registry := NewToolRegistry()
	err := registry.RegisterFromHandler("no_arg_tool", "无参数工具", noArgumentToolHandler)
	if err == nil {
		t.Fatal("expected register error for no-argument handler")
	}
}

// TestToolRegistry_RegisterFromHandlerRejectsNonStructArgument 验证非结构体参数函数注册失败。
func TestToolRegistry_RegisterFromHandlerRejectsNonStructArgument(t *testing.T) {
	registry := NewToolRegistry()
	err := registry.RegisterFromHandler("string_arg_tool", "字符串参数工具", stringArgumentToolHandler)
	if err == nil {
		t.Fatal("expected register error for non-struct argument handler")
	}
}

