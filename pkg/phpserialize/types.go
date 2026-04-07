package phpserialize

import "strconv"

type Object struct {
	ClassName  string
	Properties map[string]any
}

type Array struct {
	Keys   []any // int64 or string
	Values []any
}

func (a *Array) Len() int { return len(a.Keys) }

func (a *Array) IsSequential() bool {
	for i, k := range a.Keys {
		idx, ok := k.(int64)
		if !ok || idx != int64(i) {
			return false
		}
	}
	return true
}

func (a *Array) ToSlice() []any {
	if !a.IsSequential() {
		return nil
	}
	return a.Values
}

func (a *Array) ToMap() map[string]any {
	m := make(map[string]any, len(a.Keys))
	for i, k := range a.Keys {
		switch key := k.(type) {
		case string:
			m[key] = a.Values[i]
		case int64:
			m[strconv.FormatInt(key, 10)] = a.Values[i]
		}
	}
	return m
}

type Visibility int

const (
	Public Visibility = iota
	Protected
	Private
)

type PropertyInfo struct {
	Name       string
	Visibility Visibility
	ClassName  string // only set for Private
}
