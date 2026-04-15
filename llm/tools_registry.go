package llm
import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"
)

// ToolFunc 定义工具执行函数签名。
type ToolFunc func(arguments map[string]any) (string, error)

// ToolSpec 定义显式工具描述与执行器。
type ToolSpec struct {
	// Name 是工具名称。
	Name string
	// Description 是工具描述。
	Description string
	// Parameters 是 JSON Schema 风格参数定义。
	Parameters map[string]any
	// Executor 是工具执行函数。
	Executor ToolFunc
}

// StructuredTool 定义结构体工具注册接口。
type StructuredTool interface {
	// Name 返回工具名称。
	Name() string
	// Description 返回工具描述。
	Description() string
	// Handler 返回工具处理函数。
	Handler() any
}

// ToolRegistry 负责统一注册工具定义和执行器。
type ToolRegistry struct {
	tools  []ToolDefinition
	mapper map[string]ToolFunc
	seen   map[string]struct{}
}

// NewToolRegistry 创建空工具注册器。
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{
		tools:  make([]ToolDefinition, 0),
		mapper: make(map[string]ToolFunc),
		seen:   make(map[string]struct{}),
	}
}

// RegisterFromHandler 根据函数签名注册工具并自动生成参数 schema。
func (r *ToolRegistry) RegisterFromHandler(name, description string, handler any) error {
	if r == nil {
		return fmt.Errorf("tool registry is nil")
	}
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("tool name cannot be empty")
	}
	if _, exists := r.seen[name]; exists {
		return fmt.Errorf("duplicate tool name: %s", name)
	}

	handlerValue := reflect.ValueOf(handler)
	if !handlerValue.IsValid() {
		return fmt.Errorf("tool handler for %s is nil", name)
	}

	handlerType := handlerValue.Type()
	if handlerType.Kind() != reflect.Func {
		return fmt.Errorf("tool handler for %s must be a function", name)
	}
	if handlerType.NumOut() != 2 {
		return fmt.Errorf("tool handler for %s must return (string, error)", name)
	}
	if handlerType.Out(0).Kind() != reflect.String {
		return fmt.Errorf("tool handler for %s must return string as first value", name)
	}
	if !handlerType.Out(1).Implements(reflect.TypeFor[error]()) {
		return fmt.Errorf("tool handler for %s must return error as second value", name)
	}
	if handlerType.NumIn() != 1 {
		return fmt.Errorf("tool handler for %s must accept exactly one struct argument", name)
	}

	inputType := handlerType.In(0)
	for inputType.Kind() == reflect.Pointer {
		inputType = inputType.Elem()
	}
	if inputType.Kind() != reflect.Struct {
		return fmt.Errorf("tool handler for %s argument must be a struct", name)
	}

	parameters, err := buildToolParameters(inputType)
	if err != nil {
		return fmt.Errorf("build parameters for %s failed: %w", name, err)
	}

	executor, err := buildToolExecutor(handlerValue, handlerType)
	if err != nil {
		return fmt.Errorf("build executor for %s failed: %w", name, err)
	}

	r.tools = append(r.tools, ToolDefinition{
		Type: "function",
		Function: ToolFunction{
			Name:        name,
			Description: description,
			Parameters:  parameters,
		},
	})
	r.mapper[name] = executor
	r.seen[name] = struct{}{}
	return nil
}

// RegisterSpec 通过显式 ToolSpec 注册工具。
func (r *ToolRegistry) RegisterSpec(spec ToolSpec) error {
	if r == nil {
		return fmt.Errorf("tool registry is nil")
	}
	name := strings.TrimSpace(spec.Name)
	if name == "" {
		return fmt.Errorf("tool name cannot be empty")
	}
	if spec.Executor == nil {
		return fmt.Errorf("tool executor cannot be nil")
	}
	if _, exists := r.seen[name]; exists {
		return fmt.Errorf("duplicate tool name: %s", name)
	}

	params := spec.Parameters
	if params == nil {
		params = map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		}
	}

	r.tools = append(r.tools, ToolDefinition{
		Type: "function",
		Function: ToolFunction{
			Name:        name,
			Description: spec.Description,
			Parameters:  params,
		},
	})
	r.mapper[name] = spec.Executor
	r.seen[name] = struct{}{}
	return nil
}

// RegisterStructuredTool 通过结构体工具描述自动注册。
func (r *ToolRegistry) RegisterStructuredTool(tool StructuredTool) error {
	if tool == nil {
		return fmt.Errorf("structured tool is nil")
	}
	return r.RegisterFromHandler(tool.Name(), tool.Description(), tool.Handler())
}

// Definitions 返回工具定义副本。
func (r *ToolRegistry) Definitions() []ToolDefinition {
	if r == nil {
		return nil
	}
	result := make([]ToolDefinition, len(r.tools))
	copy(result, r.tools)
	return result
}

// Mapper 返回工具执行映射副本。
func (r *ToolRegistry) Mapper() map[string]ToolFunc {
	if r == nil {
		return nil
	}
	result := make(map[string]ToolFunc, len(r.mapper))
	for name, tool := range r.mapper {
		result[name] = tool
	}
	return result
}

// buildToolExecutor 构建统一 ToolFunc 执行器。
func buildToolExecutor(handlerValue reflect.Value, handlerType reflect.Type) (ToolFunc, error) {
	return func(arguments map[string]any) (string, error) {
		callArgs, err := buildCallArguments(handlerType, arguments)
		if err != nil {
			return "", err
		}
		results := handlerValue.Call(callArgs)
		output := results[0].Interface().(string)
		if err, ok := results[1].Interface().(error); ok && err != nil {
			return output, err
		}
		return output, nil
	}, nil
}

// buildCallArguments 根据处理函数签名转换调用参数。
func buildCallArguments(handlerType reflect.Type, arguments map[string]any) ([]reflect.Value, error) {
	if handlerType.NumIn() == 0 {
		return nil, nil
	}
	if handlerType.NumIn() != 1 {
		return nil, fmt.Errorf("unsupported handler signature")
	}

	inputType := handlerType.In(0)
	inputValue, err := decodeToolInput(inputType, arguments)
	if err != nil {
		return nil, err
	}
	return []reflect.Value{inputValue}, nil
}

// decodeToolInput 把 map 参数解码为目标处理函数参数。
func decodeToolInput(inputType reflect.Type, arguments map[string]any) (reflect.Value, error) {
	if inputType.Kind() == reflect.Pointer {
		value, err := decodeToolInput(inputType.Elem(), arguments)
		if err != nil {
			return reflect.Value{}, err
		}
		pointer := reflect.New(inputType.Elem())
		pointer.Elem().Set(value)
		return pointer, nil
	}

	if inputType.Kind() == reflect.Map && inputType.Key().Kind() == reflect.String && inputType.Elem().Kind() == reflect.Interface {
		if arguments == nil {
			return reflect.MakeMapWithSize(inputType, 0), nil
		}
		value := reflect.MakeMapWithSize(inputType, len(arguments))
		for key, item := range arguments {
			value.SetMapIndex(reflect.ValueOf(key), reflect.ValueOf(item))
		}
		return value, nil
	}

	jsonBytes, err := json.Marshal(arguments)
	if err != nil {
		return reflect.Value{}, fmt.Errorf("marshal tool arguments failed: %w", err)
	}

	inputValue := reflect.New(inputType)
	if len(jsonBytes) > 0 {
		if err := json.Unmarshal(jsonBytes, inputValue.Interface()); err != nil {
			return reflect.Value{}, fmt.Errorf("decode tool arguments failed: %w", err)
		}
	}
	return inputValue.Elem(), nil
}

// buildToolParameters 根据参数类型构建 JSON Schema。
func buildToolParameters(inputType reflect.Type) (map[string]any, error) {
	if inputType.Kind() == reflect.Pointer {
		return buildToolParameters(inputType.Elem())
	}

	if inputType == reflect.TypeOf(time.Time{}) {
		return map[string]any{"type": "string", "format": "date-time"}, nil
	}

	if inputType.Kind() == reflect.Struct {
		return buildObjectSchema(inputType)
	}

	if inputType.Kind() == reflect.Map && inputType.Key().Kind() == reflect.String {
		valueSchema, err := schemaForType(inputType.Elem())
		if err != nil {
			return nil, err
		}
		return map[string]any{"type": "object", "additionalProperties": valueSchema}, nil
	}

	if inputType.Kind() == reflect.Interface && inputType.NumMethod() == 0 {
		return map[string]any{"type": "object", "additionalProperties": true}, nil
	}

	if inputType.Kind() == reflect.Invalid {
		return map[string]any{"type": "object"}, nil
	}

	if inputType.Kind() == reflect.Func || inputType.Kind() == reflect.Chan {
		return nil, fmt.Errorf("unsupported input type: %s", inputType.Kind())
	}

	valueSchema, err := schemaForType(inputType)
	if err != nil {
		return nil, err
	}
	return map[string]any{"type": "object", "properties": map[string]any{"value": valueSchema}, "required": []string{"value"}}, nil
}

// buildObjectSchema 基于 struct 字段构建 object schema。
func buildObjectSchema(inputType reflect.Type) (map[string]any, error) {
	properties := make(map[string]any)
	required := make([]string, 0)

	for fieldIndex := 0; fieldIndex < inputType.NumField(); fieldIndex++ {
		field := inputType.Field(fieldIndex)
		if !field.IsExported() {
			continue
		}

		fieldName, skip := jsonFieldName(field)
		if skip {
			continue
		}

		fieldSchema, err := schemaForType(field.Type)
		if err != nil {
			return nil, fmt.Errorf("field %s: %w", field.Name, err)
		}

		fieldSchema = applyToolFieldTags(fieldSchema, field)
		properties[fieldName] = fieldSchema

		if isFieldRequired(field) {
			required = append(required, fieldName)
		}
	}

	schema := map[string]any{"type": "object", "properties": properties}
	if len(required) > 0 {
		sort.Strings(required)
		schema["required"] = required
	}
	return schema, nil
}

// schemaForType 把 Go 类型映射为 JSON Schema 片段。
func schemaForType(t reflect.Type) (map[string]any, error) {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	switch t.Kind() {
	case reflect.String:
		return map[string]any{"type": "string"}, nil
	case reflect.Bool:
		return map[string]any{"type": "boolean"}, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return map[string]any{"type": "integer"}, nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return map[string]any{"type": "integer"}, nil
	case reflect.Float32, reflect.Float64:
		return map[string]any{"type": "number"}, nil
	case reflect.Slice, reflect.Array:
		items, err := schemaForType(t.Elem())
		if err != nil {
			return nil, err
		}
		return map[string]any{"type": "array", "items": items}, nil
	case reflect.Map:
		if t.Key().Kind() != reflect.String {
			return nil, fmt.Errorf("only string-keyed maps are supported, got %s", t.Key().Kind())
		}
		properties, err := schemaForType(t.Elem())
		if err != nil {
			return nil, err
		}
		return map[string]any{"type": "object", "additionalProperties": properties}, nil
	case reflect.Struct:
		if t.PkgPath() == "time" && t.Name() == "Time" {
			return map[string]any{"type": "string", "format": "date-time"}, nil
		}
		return buildObjectSchema(t)
	case reflect.Interface:
		if t.NumMethod() == 0 {
			return map[string]any{"type": "object"}, nil
		}
		return nil, fmt.Errorf("unsupported interface type: %s", t.String())
	default:
		return nil, fmt.Errorf("unsupported schema type: %s", t.Kind())
	}
}

// jsonFieldName 返回字段 json 名称并标记是否忽略。
func jsonFieldName(field reflect.StructField) (string, bool) {
	tag := field.Tag.Get("json")
	if tag == "-" {
		return "", true
	}
	if tag == "" {
		return strings.ToLower(field.Name[:1]) + field.Name[1:], false
	}
	parts := strings.Split(tag, ",")
	name := strings.TrimSpace(parts[0])
	if name == "" {
		return strings.ToLower(field.Name[:1]) + field.Name[1:], false
	}
	return name, false
}

// isFieldRequired 根据 tag 和类型判断字段是否必填。
func isFieldRequired(field reflect.StructField) bool {
	toolTag := parseToolTag(field.Tag.Get("tool"))
	if toolTag.required {
		return true
	}
	if field.Type.Kind() == reflect.Pointer {
		return false
	}
	jsonTag := field.Tag.Get("json")
	if jsonTag == "" {
		return true
	}
	parts := strings.Split(jsonTag, ",")
	for _, part := range parts[1:] {
		if part == "omitempty" {
			return false
		}
	}
	return true
}

// applyToolFieldTags 把 tool 标签写入字段 schema。
func applyToolFieldTags(schema map[string]any, field reflect.StructField) map[string]any {
	if schema == nil {
		return nil
	}
	toolTag := parseToolTag(field.Tag.Get("tool"))
	if toolTag.description != "" {
		schema["description"] = toolTag.description
	}
	if toolTag.defaultValue != nil {
		schema["default"] = toolTag.defaultValue
	}
	if len(toolTag.enumValues) > 0 {
		schema["enum"] = toolTag.enumValues
	}
	if toolTag.format != "" {
		schema["format"] = toolTag.format
	}
	return schema
}

// toolTagOptions 是 tool 标签解析结果。
type toolTagOptions struct {
	// description 是字段说明。
	description string
	// required 指示字段是否必填。
	required bool
	// defaultValue 是默认值。
	defaultValue any
	// enumValues 是枚举值列表。
	enumValues []string
	// format 是格式标记。
	format string
}

// parseToolTag 解析形如 desc=xxx;required;default=yyy 的标签。
func parseToolTag(raw string) toolTagOptions {
	options := toolTagOptions{}
	for _, item := range strings.Split(raw, ";") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		key, value, found := strings.Cut(item, "=")
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if !found {
			if key == "required" {
				options.required = true
			}
			continue
		}

		switch key {
		case "desc", "description":
			options.description = value
		case "default":
			options.defaultValue = value
		case "enum":
			options.enumValues = strings.Split(value, "|")
		case "format":
			options.format = value
		}
	}
	return options
}

