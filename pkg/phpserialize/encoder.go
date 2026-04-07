package phpserialize

import (
	"fmt"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

func Encode(v any) (string, error) {
	var b strings.Builder
	if err := encodeValue(&b, v); err != nil {
		return "", err
	}
	return b.String(), nil
}

func MarshalObject(className string, properties map[string]any) (string, error) {
	return Encode(&Object{ClassName: className, Properties: properties})
}

func encodeValue(b *strings.Builder, v any) error {
	if v == nil {
		b.WriteString("N;")
		return nil
	}

	switch val := v.(type) {
	case bool:
		if val {
			b.WriteString("b:1;")
		} else {
			b.WriteString("b:0;")
		}
	case int:
		writeInt(b, int64(val))
	case int8:
		writeInt(b, int64(val))
	case int16:
		writeInt(b, int64(val))
	case int32:
		writeInt(b, int64(val))
	case int64:
		writeInt(b, val)
	case uint:
		writeUint(b, uint64(val))
	case uint8:
		writeUint(b, uint64(val))
	case uint16:
		writeUint(b, uint64(val))
	case uint32:
		writeUint(b, uint64(val))
	case uint64:
		writeUint(b, val)
	case float32:
		encodeFloat(b, float64(val))
	case float64:
		encodeFloat(b, val)
	case string:
		encodeString(b, val)
	case []any:
		return encodeSlice(b, val)
	case []string:
		items := make([]any, len(val))
		for i, s := range val {
			items[i] = s
		}
		return encodeSlice(b, items)
	case []int:
		items := make([]any, len(val))
		for i, n := range val {
			items[i] = n
		}
		return encodeSlice(b, items)
	case map[string]any:
		return encodeMap(b, val)
	case *Object:
		return encodeObject(b, val)
	case *Array:
		return encodeArray(b, val)
	default:
		rv := reflect.ValueOf(v)
		switch rv.Kind() {
		case reflect.Slice, reflect.Array:
			items := make([]any, rv.Len())
			for i := range rv.Len() {
				items[i] = rv.Index(i).Interface()
			}
			return encodeSlice(b, items)
		case reflect.Map:
			m := make(map[string]any, rv.Len())
			for _, k := range rv.MapKeys() {
				m[fmt.Sprintf("%v", k.Interface())] = rv.MapIndex(k).Interface()
			}
			return encodeMap(b, m)
		default:
			return fmt.Errorf("phpserialize: unsupported type %T", v)
		}
	}
	return nil
}

func writeInt(b *strings.Builder, val int64) {
	b.WriteString("i:")
	b.WriteString(strconv.FormatInt(val, 10))
	b.WriteByte(';')
}

func writeUint(b *strings.Builder, val uint64) {
	b.WriteString("i:")
	b.WriteString(strconv.FormatUint(val, 10))
	b.WriteByte(';')
}

func encodeFloat(b *strings.Builder, val float64) {
	b.WriteString("d:")
	if math.IsInf(val, 1) {
		b.WriteString("INF")
	} else if math.IsInf(val, -1) {
		b.WriteString("-INF")
	} else if math.IsNaN(val) {
		b.WriteString("NAN")
	} else {
		b.WriteString(strconv.FormatFloat(val, 'f', -1, 64))
	}
	b.WriteByte(';')
}

func encodeString(b *strings.Builder, val string) {
	b.WriteString("s:")
	b.WriteString(strconv.Itoa(len(val)))
	b.WriteString(":\"")
	b.WriteString(val)
	b.WriteString("\";")
}

func encodeSlice(b *strings.Builder, val []any) error {
	b.WriteString("a:")
	b.WriteString(strconv.Itoa(len(val)))
	b.WriteString(":{")
	for i, item := range val {
		writeInt(b, int64(i))
		if err := encodeValue(b, item); err != nil {
			return err
		}
	}
	b.WriteByte('}')
	return nil
}

func encodeSortedProperties(b *strings.Builder, properties map[string]any) error {
	keys := make([]string, 0, len(properties))
	for k := range properties {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		encodeString(b, k)
		if err := encodeValue(b, properties[k]); err != nil {
			return err
		}
	}
	return nil
}

func encodeMap(b *strings.Builder, val map[string]any) error {
	b.WriteString("a:")
	b.WriteString(strconv.Itoa(len(val)))
	b.WriteString(":{")
	if err := encodeSortedProperties(b, val); err != nil {
		return err
	}
	b.WriteByte('}')
	return nil
}

func encodeObject(b *strings.Builder, obj *Object) error {
	b.WriteString("O:")
	b.WriteString(strconv.Itoa(len(obj.ClassName)))
	b.WriteString(":\"")
	b.WriteString(obj.ClassName)
	b.WriteString("\":")
	b.WriteString(strconv.Itoa(len(obj.Properties)))
	b.WriteString(":{")
	if err := encodeSortedProperties(b, obj.Properties); err != nil {
		return err
	}
	b.WriteByte('}')
	return nil
}

func encodeArray(b *strings.Builder, arr *Array) error {
	b.WriteString("a:")
	b.WriteString(strconv.Itoa(len(arr.Keys)))
	b.WriteString(":{")
	for i, k := range arr.Keys {
		switch key := k.(type) {
		case int64:
			writeInt(b, key)
		case string:
			encodeString(b, key)
		default:
			return fmt.Errorf("phpserialize: unsupported array key type %T", k)
		}
		if err := encodeValue(b, arr.Values[i]); err != nil {
			return err
		}
	}
	b.WriteByte('}')
	return nil
}
