package utils

import (
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
)

// PrintMessageList 将消息列表中每条消息的所有字段打印到标准输出，用于调试。
//
// 该函数支持传入结构体切片（如 []Message）或其指针；当入参不是切片/数组时返回错误。
func PrintMessageList(messages any) error {
	return PrintMessageListToWriter(os.Stdout, messages)
}

// PrintMessageListToWriter 将消息列表中每条消息的所有字段打印到指定输出流。
//
// 打印内容仅包含字段名和值，便于快速核对上下文消息结构。
func PrintMessageListToWriter(w io.Writer, messages any) error {
	if w == nil {
		return fmt.Errorf("writer cannot be nil")
	}

	value := reflect.ValueOf(messages)
	if !value.IsValid() {
		fmt.Fprintln(w, "message 列表为空(nil)")
		return nil
	}

	for value.Kind() == reflect.Ptr {
		if value.IsNil() {
			fmt.Fprintln(w, "message 列表为空(nil)")
			return nil
		}
		value = value.Elem()
	}

	kind := value.Kind()
	if kind != reflect.Slice && kind != reflect.Array {
		return fmt.Errorf("messages must be a slice or array, got %s", kind)
	}

	fmt.Fprintf(w, "message 总数: %d\n", value.Len())
	for i := range value.Len() {
		item := value.Index(i)
		for item.Kind() == reflect.Ptr || item.Kind() == reflect.Interface {
			if item.IsNil() {
				break
			}
			item = item.Elem()
		}

		fmt.Fprintf(w, "message[%d]:\n", i)
		if item.Kind() != reflect.Struct {
			fmt.Fprintf(w, "  value = %#v\n", item.Interface())
			continue
		}

		itemType := item.Type()
		for fieldIndex := range item.NumField() {
			fieldType := itemType.Field(fieldIndex)
			fieldValue := item.Field(fieldIndex)

			fmt.Fprintf(
				w,
				"  %s = %s\n",
				fieldType.Name,
				formatReflectValue(fieldValue),
			)
		}
	}

	return nil
}

// formatReflectValue 将反射值格式化为便于阅读的调试文本。
func formatReflectValue(v reflect.Value) string {
	if !v.IsValid() {
		return "<invalid>"
	}

	if v.Kind() == reflect.String {
		return fmt.Sprintf("%q", v.String())
	}

	return strings.TrimSpace(fmt.Sprintf("%#v", v.Interface()))
}
