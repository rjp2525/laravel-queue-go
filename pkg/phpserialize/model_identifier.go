package phpserialize

// ModelIdentifierClass is the PHP FQCN for Laravel's ModelIdentifier.
const ModelIdentifierClass = `Illuminate\Contracts\Database\ModelIdentifier`

type ModelIdentifier struct {
	Class           string
	ID              any // int64, string, or []any for collections
	Relations       []string
	Connection      string
	CollectionClass *string
}

func IsModelIdentifier(obj *Object) bool {
	return obj != nil && obj.ClassName == ModelIdentifierClass
}

func AsModelIdentifier(v any) *ModelIdentifier {
	obj, ok := v.(*Object)
	if !ok || !IsModelIdentifier(obj) {
		return nil
	}

	mid := &ModelIdentifier{
		Class:      GetString(obj, "class"),
		ID:         obj.Properties["id"],
		Connection: GetString(obj, "connection"),
	}

	if rels, ok := obj.Properties["relations"].(*Array); ok {
		for _, v := range rels.Values {
			if s, ok := v.(string); ok {
				mid.Relations = append(mid.Relations, s)
			}
		}
	}

	if cc, ok := obj.Properties["collectionClass"].(string); ok {
		mid.CollectionClass = &cc
	}

	return mid
}

func (m *ModelIdentifier) ToObject() *Object {
	props := map[string]any{
		"class":      m.Class,
		"id":         m.ID,
		"connection": m.Connection,
	}

	if m.CollectionClass != nil {
		props["collectionClass"] = *m.CollectionClass
	} else {
		props["collectionClass"] = nil
	}

	rels := &Array{
		Keys:   make([]any, len(m.Relations)),
		Values: make([]any, len(m.Relations)),
	}
	for i, r := range m.Relations {
		rels.Keys[i] = int64(i)
		rels.Values[i] = r
	}
	props["relations"] = rels

	return &Object{
		ClassName:  ModelIdentifierClass,
		Properties: props,
	}
}
