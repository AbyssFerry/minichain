package main

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"
)

// toolCatalog 统一保存工具定义与本地执行映射，避免手写两份数据结构。
type toolCatalog struct {
	// tools 是发送给模型的工具定义列表。
	tools []ToolDefinition
	// mapper 是工具名到本地执行函数的映射表。
	mapper map[string]ToolFunc
}

// ToolRegistry 负责根据 Go 函数自动生成工具定义，并包装成可执行的本地函数。
type ToolRegistry struct {
	tools  []ToolDefinition
	mapper map[string]ToolFunc
	seen   map[string]struct{}
}

// newToolRegistry 创建一个空的工具注册器。
func newToolRegistry() *ToolRegistry {
	return &ToolRegistry{
		tools:  make([]ToolDefinition, 0),
		mapper: make(map[string]ToolFunc),
		seen:   make(map[string]struct{}),
	}
}

// RegisterFromHandler 根据函数签名注册一个工具，并自动生成 JSON Schema 风格参数定义。
//
// handler 必须是以下形态之一：
// - func() (string, error)
// - func(T) (string, error)
// - func(*T) (string, error)
// 其中 T 建议为带 json tag 的 struct，便于自动生成可读的参数 schema。
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
	if handlerType.NumIn() > 1 {
		return fmt.Errorf("tool handler for %s must accept zero or one argument", name)
	}

	inputType := reflect.TypeFor[struct{}]()
	if handlerType.NumIn() == 1 {
		inputType = handlerType.In(0)
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

// Definitions 返回注册器中的工具定义副本。
func (r *ToolRegistry) Definitions() []ToolDefinition {
	if r == nil {
		return nil
	}
	result := make([]ToolDefinition, len(r.tools))
	copy(result, r.tools)
	return result
}

// Mapper 返回注册器中的工具执行映射副本。
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

// defaultToolCatalogOnce 用于缓存默认工具目录，避免每次调用重复反射构建。
var defaultToolCatalogOnce sync.Once

// defaultToolCatalogValue 保存默认工具目录的构建结果。
var defaultToolCatalogValue toolCatalog

// defaultToolCatalogErr 保存默认工具目录构建过程中的错误。
var defaultToolCatalogErr error

// defaultToolCatalog 构建默认工具目录。
func defaultToolCatalog() (toolCatalog, error) {
	defaultToolCatalogOnce.Do(func() {
		registry := newToolRegistry()
		if err := registry.RegisterFromHandler(
			"get_current_time",
			"当你想知道现在时间时非常有用。",
			getCurrentTime,
		); err != nil {
			defaultToolCatalogErr = err
			return
		}
		if err := registry.RegisterFromHandler(
			"get_current_weather",
			"当你想查询指定城市天气时非常有用。",
			getCurrentWeather,
		); err != nil {
			defaultToolCatalogErr = err
			return
		}

		defaultToolCatalogValue = toolCatalog{
			tools:  registry.Definitions(),
			mapper: registry.Mapper(),
		}
	})
	return defaultToolCatalogValue, defaultToolCatalogErr
}

// defaultTools 返回默认注册给模型的函数工具定义。
func defaultTools() []ToolDefinition {
	catalog, err := defaultToolCatalog()
	if err != nil {
		panic(err)
	}
	return cloneToolDefinitions(catalog.tools)
}

// defaultToolMapper 返回工具名到本地处理函数的映射表。
func defaultToolMapper() map[string]ToolFunc {
	catalog, err := defaultToolCatalog()
	if err != nil {
		panic(err)
	}
	return cloneToolMapper(catalog.mapper)
}

// buildToolExecutor 将一个带强类型参数的函数包装成运行时可执行的 ToolFunc。
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

// buildCallArguments 根据目标函数签名，将 map 参数转换成可调用参数列表。
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

// decodeToolInput 将通用参数对象解码成 handler 所需的具体输入值。
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

// buildToolParameters 根据参数类型构建 JSON Schema 风格参数定义。
func buildToolParameters(inputType reflect.Type) (map[string]any, error) {
	if inputType.Kind() == reflect.Pointer {
		return buildToolParameters(inputType.Elem())
	}

	if inputType == reflect.TypeOf(time.Time{}) {
		return map[string]any{
			"type":   "string",
			"format": "date-time",
		}, nil
	}

	if inputType.Kind() == reflect.Struct {
		return buildObjectSchema(inputType)
	}

	if inputType.Kind() == reflect.Map && inputType.Key().Kind() == reflect.String {
		valueSchema, err := schemaForType(inputType.Elem())
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"type":                 "object",
			"additionalProperties": valueSchema,
		}, nil
	}

	if inputType.Kind() == reflect.Interface && inputType.NumMethod() == 0 {
		return map[string]any{
			"type":                 "object",
			"additionalProperties": true,
		}, nil
	}

	if inputType.Kind() == reflect.Invalid {
		return map[string]any{"type": "object"}, nil
	}

	if inputType.Kind() == reflect.Func || inputType.Kind() == reflect.Chan {
		return nil, fmt.Errorf("unsupported input type: %s", inputType.Kind())
	}

	// 对于非结构体输入，仍包装成单一属性对象，避免模型调用时结构不稳定。
	valueSchema, err := schemaForType(inputType)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{"value": valueSchema},
		"required":   []string{"value"},
	}, nil
}

// buildObjectSchema 根据 struct 生成 object schema。
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

	schema := map[string]any{
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		// 保持 required 顺序可预测，便于测试和调试。
		sort.Strings(required)
		schema["required"] = required
	}
	return schema, nil
}

// schemaForType 生成单个 Go 类型对应的 JSON Schema 片段。
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

// jsonFieldName 返回字段对应的 JSON 名称，并标记是否应跳过。
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

// isFieldRequired 根据 JSON tag 与字段类型判断字段是否必填。
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

// applyToolFieldTags 将字段上的 tool tag 解析并合并进 schema。
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

// toolTagOptions 保存 tool tag 解析结果。
type toolTagOptions struct {
	description  string
	required     bool
	defaultValue any
	enumValues   []string
	format       string
}

// parseToolTag 解析形如 "desc=xxx;required;default=yyy;enum=a|b" 的 tag。
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

// cloneToolDefinitions 返回工具定义的浅拷贝，避免外部修改内部缓存。
func cloneToolDefinitions(definitions []ToolDefinition) []ToolDefinition {
	if len(definitions) == 0 {
		return nil
	}
	result := make([]ToolDefinition, len(definitions))
	copy(result, definitions)
	return result
}

// cloneToolMapper 返回工具映射表的浅拷贝，避免外部修改内部缓存。
func cloneToolMapper(mapper map[string]ToolFunc) map[string]ToolFunc {
	if len(mapper) == 0 {
		return nil
	}
	result := make(map[string]ToolFunc, len(mapper))
	for name, tool := range mapper {
		result[name] = tool
	}
	return result
}
