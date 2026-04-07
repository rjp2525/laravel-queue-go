package phpserialize

func GetString(obj *Object, key string) string {
	if obj == nil {
		return ""
	}
	if v, ok := obj.Properties[key].(string); ok {
		return v
	}
	return ""
}

func GetInt(obj *Object, key string) int64 {
	if obj == nil {
		return 0
	}
	v := obj.Properties[key]
	switch val := v.(type) {
	case int64:
		return val
	case float64:
		return int64(val)
	case int:
		return int64(val)
	}
	return 0
}

func GetFloat(obj *Object, key string) float64 {
	if obj == nil {
		return 0
	}
	v := obj.Properties[key]
	switch val := v.(type) {
	case float64:
		return val
	case int64:
		return float64(val)
	}
	return 0
}

func GetBool(obj *Object, key string) bool {
	if obj == nil {
		return false
	}
	if v, ok := obj.Properties[key].(bool); ok {
		return v
	}
	return false
}

func GetSlice(obj *Object, key string) []any {
	if obj == nil {
		return nil
	}
	v := obj.Properties[key]
	switch val := v.(type) {
	case *Array:
		return val.Values
	case []any:
		return val
	}
	return nil
}

func GetMap(obj *Object, key string) map[string]any {
	if obj == nil {
		return nil
	}
	v := obj.Properties[key]
	switch val := v.(type) {
	case *Array:
		return val.ToMap()
	case map[string]any:
		return val
	}
	return nil
}
