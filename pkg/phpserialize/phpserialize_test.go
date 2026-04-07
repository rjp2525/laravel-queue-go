package phpserialize

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Decode tests
// ---------------------------------------------------------------------------

func TestDecodeNull(t *testing.T) {
	val, err := Decode("N;")
	require.NoError(t, err)
	assert.Nil(t, val)
}

func TestDecodeBoolTrue(t *testing.T) {
	val, err := Decode("b:1;")
	require.NoError(t, err)
	assert.Equal(t, true, val)
}

func TestDecodeBoolFalse(t *testing.T) {
	val, err := Decode("b:0;")
	require.NoError(t, err)
	assert.Equal(t, false, val)
}

func TestDecodeInt(t *testing.T) {
	val, err := Decode("i:42;")
	require.NoError(t, err)
	assert.Equal(t, int64(42), val)
}

func TestDecodeNegativeInt(t *testing.T) {
	val, err := Decode("i:-7;")
	require.NoError(t, err)
	assert.Equal(t, int64(-7), val)
}

func TestDecodeZeroInt(t *testing.T) {
	val, err := Decode("i:0;")
	require.NoError(t, err)
	assert.Equal(t, int64(0), val)
}

func TestDecodeFloat(t *testing.T) {
	val, err := Decode("d:3.14;")
	require.NoError(t, err)
	assert.InDelta(t, 3.14, val, 1e-9)
}

func TestDecodeFloatNegative(t *testing.T) {
	val, err := Decode("d:-2.5;")
	require.NoError(t, err)
	assert.InDelta(t, -2.5, val, 1e-9)
}

func TestDecodeFloatInf(t *testing.T) {
	val, err := Decode("d:INF;")
	require.NoError(t, err)
	assert.True(t, math.IsInf(val.(float64), 1))
}

func TestDecodeFloatNegInf(t *testing.T) {
	val, err := Decode("d:-INF;")
	require.NoError(t, err)
	assert.True(t, math.IsInf(val.(float64), -1))
}

func TestDecodeFloatNaN(t *testing.T) {
	val, err := Decode("d:NAN;")
	require.NoError(t, err)
	assert.True(t, math.IsNaN(val.(float64)))
}

func TestDecodeString(t *testing.T) {
	val, err := Decode(`s:5:"hello";`)
	require.NoError(t, err)
	assert.Equal(t, "hello", val)
}

func TestDecodeEmptyString(t *testing.T) {
	val, err := Decode(`s:0:"";`)
	require.NoError(t, err)
	assert.Equal(t, "", val)
}

func TestDecodeSequentialArray(t *testing.T) {
	val, err := Decode(`a:3:{i:0;s:1:"a";i:1;s:1:"b";i:2;s:1:"c";}`)
	require.NoError(t, err)
	arr, ok := val.(*Array)
	require.True(t, ok)
	assert.True(t, arr.IsSequential())
	assert.Equal(t, 3, arr.Len())
	assert.Equal(t, []any{"a", "b", "c"}, arr.ToSlice())
}

func TestDecodeAssocArray(t *testing.T) {
	val, err := Decode(`a:2:{s:3:"foo";i:1;s:3:"bar";i:2;}`)
	require.NoError(t, err)
	arr, ok := val.(*Array)
	require.True(t, ok)
	m := arr.ToMap()
	assert.Equal(t, int64(1), m["foo"])
	assert.Equal(t, int64(2), m["bar"])
}

func TestDecodeMixedKeyArray(t *testing.T) {
	// Array with both integer and string keys.
	val, err := Decode(`a:2:{i:0;s:5:"first";s:3:"key";s:5:"value";}`)
	require.NoError(t, err)
	arr, ok := val.(*Array)
	require.True(t, ok)
	assert.False(t, arr.IsSequential())
	m := arr.ToMap()
	assert.Equal(t, "first", m["0"])
	assert.Equal(t, "value", m["key"])
}

func TestDecodeEmptyArray(t *testing.T) {
	val, err := Decode(`a:0:{}`)
	require.NoError(t, err)
	arr, ok := val.(*Array)
	require.True(t, ok)
	assert.Equal(t, 0, arr.Len())
}

func TestDecodeObject(t *testing.T) {
	val, err := Decode(`O:8:"stdClass":2:{s:4:"name";s:3:"Bob";s:3:"age";i:30;}`)
	require.NoError(t, err)
	obj, ok := val.(*Object)
	require.True(t, ok)
	assert.Equal(t, "stdClass", obj.ClassName)
	assert.Equal(t, "Bob", obj.Properties["name"])
	assert.Equal(t, int64(30), obj.Properties["age"])
}

func TestDecodeNestedObject(t *testing.T) {
	data := `O:5:"Outer":1:{s:5:"inner";O:5:"Inner":1:{s:1:"x";i:1;}}`
	val, err := Decode(data)
	require.NoError(t, err)
	outer, ok := val.(*Object)
	require.True(t, ok)
	inner, ok := outer.Properties["inner"].(*Object)
	require.True(t, ok)
	assert.Equal(t, "Inner", inner.ClassName)
	assert.Equal(t, int64(1), inner.Properties["x"])
}

func TestDecodeBytes(t *testing.T) {
	val, err := DecodeBytes([]byte("i:99;"))
	require.NoError(t, err)
	assert.Equal(t, int64(99), val)
}

// ---------------------------------------------------------------------------
// Visibility stripping
// ---------------------------------------------------------------------------

func TestStripVisibilityPublic(t *testing.T) {
	assert.Equal(t, "name", stripVisibility("name"))
}

func TestStripVisibilityProtected(t *testing.T) {
	assert.Equal(t, "name", stripVisibility("\x00*\x00name"))
}

func TestStripVisibilityPrivate(t *testing.T) {
	assert.Equal(t, "name", stripVisibility("\x00SomeClass\x00name"))
}

func TestParsePropertyNamePublic(t *testing.T) {
	pi := ParsePropertyName("foo")
	assert.Equal(t, "foo", pi.Name)
	assert.Equal(t, Public, pi.Visibility)
}

func TestParsePropertyNameProtected(t *testing.T) {
	pi := ParsePropertyName("\x00*\x00bar")
	assert.Equal(t, "bar", pi.Name)
	assert.Equal(t, Protected, pi.Visibility)
}

func TestParsePropertyNamePrivate(t *testing.T) {
	pi := ParsePropertyName("\x00MyClass\x00secret")
	assert.Equal(t, "secret", pi.Name)
	assert.Equal(t, Private, pi.Visibility)
	assert.Equal(t, "MyClass", pi.ClassName)
}

// ---------------------------------------------------------------------------
// Encode tests
// ---------------------------------------------------------------------------

func TestEncodeNil(t *testing.T) {
	s, err := Encode(nil)
	require.NoError(t, err)
	assert.Equal(t, "N;", s)
}

func TestEncodeBool(t *testing.T) {
	s, err := Encode(true)
	require.NoError(t, err)
	assert.Equal(t, "b:1;", s)

	s, err = Encode(false)
	require.NoError(t, err)
	assert.Equal(t, "b:0;", s)
}

func TestEncodeInt(t *testing.T) {
	s, err := Encode(int64(42))
	require.NoError(t, err)
	assert.Equal(t, "i:42;", s)
}

func TestEncodeIntNative(t *testing.T) {
	s, err := Encode(7)
	require.NoError(t, err)
	assert.Equal(t, "i:7;", s)
}

func TestEncodeFloat(t *testing.T) {
	s, err := Encode(3.14)
	require.NoError(t, err)
	assert.Equal(t, "d:3.14;", s)
}

func TestEncodeFloatInf(t *testing.T) {
	s, err := Encode(math.Inf(1))
	require.NoError(t, err)
	assert.Equal(t, "d:INF;", s)
}

func TestEncodeString(t *testing.T) {
	s, err := Encode("hello")
	require.NoError(t, err)
	assert.Equal(t, `s:5:"hello";`, s)
}

func TestEncodeEmptyString(t *testing.T) {
	s, err := Encode("")
	require.NoError(t, err)
	assert.Equal(t, `s:0:"";`, s)
}

func TestEncodeSlice(t *testing.T) {
	s, err := Encode([]any{"a", int64(1)})
	require.NoError(t, err)
	assert.Equal(t, `a:2:{i:0;s:1:"a";i:1;i:1;}`, s)
}

func TestEncodeStringSlice(t *testing.T) {
	s, err := Encode([]string{"x", "y"})
	require.NoError(t, err)
	assert.Equal(t, `a:2:{i:0;s:1:"x";i:1;s:1:"y";}`, s)
}

func TestEncodeMap(t *testing.T) {
	s, err := Encode(map[string]any{"a": int64(1), "b": int64(2)})
	require.NoError(t, err)
	// Keys are sorted.
	assert.Equal(t, `a:2:{s:1:"a";i:1;s:1:"b";i:2;}`, s)
}

func TestEncodeObject(t *testing.T) {
	obj := &Object{
		ClassName: "Foo",
		Properties: map[string]any{
			"x": int64(10),
		},
	}
	s, err := Encode(obj)
	require.NoError(t, err)
	assert.Equal(t, `O:3:"Foo":1:{s:1:"x";i:10;}`, s)
}

func TestEncodeArray(t *testing.T) {
	arr := &Array{
		Keys:   []any{int64(0), int64(1)},
		Values: []any{"a", "b"},
	}
	s, err := Encode(arr)
	require.NoError(t, err)
	assert.Equal(t, `a:2:{i:0;s:1:"a";i:1;s:1:"b";}`, s)
}

func TestMarshalObject(t *testing.T) {
	s, err := MarshalObject("MyClass", map[string]any{
		"name": "test",
		"val":  int64(5),
	})
	require.NoError(t, err)
	assert.Equal(t, `O:7:"MyClass":2:{s:4:"name";s:4:"test";s:3:"val";i:5;}`, s)
}

// ---------------------------------------------------------------------------
// Round-trip encode/decode consistency
// ---------------------------------------------------------------------------

func TestRoundTripInt(t *testing.T) {
	encoded, err := Encode(int64(123))
	require.NoError(t, err)
	decoded, err := Decode(encoded)
	require.NoError(t, err)
	assert.Equal(t, int64(123), decoded)
}

func TestRoundTripString(t *testing.T) {
	encoded, err := Encode("hello world")
	require.NoError(t, err)
	decoded, err := Decode(encoded)
	require.NoError(t, err)
	assert.Equal(t, "hello world", decoded)
}

func TestRoundTripBool(t *testing.T) {
	for _, b := range []bool{true, false} {
		encoded, err := Encode(b)
		require.NoError(t, err)
		decoded, err := Decode(encoded)
		require.NoError(t, err)
		assert.Equal(t, b, decoded)
	}
}

func TestRoundTripNull(t *testing.T) {
	encoded, err := Encode(nil)
	require.NoError(t, err)
	decoded, err := Decode(encoded)
	require.NoError(t, err)
	assert.Nil(t, decoded)
}

func TestRoundTripObject(t *testing.T) {
	obj := &Object{
		ClassName: "App\\Test",
		Properties: map[string]any{
			"id":   int64(1),
			"name": "test",
		},
	}
	encoded, err := Encode(obj)
	require.NoError(t, err)
	decoded, err := Decode(encoded)
	require.NoError(t, err)
	out, ok := decoded.(*Object)
	require.True(t, ok)
	assert.Equal(t, "App\\Test", out.ClassName)
	assert.Equal(t, int64(1), out.Properties["id"])
	assert.Equal(t, "test", out.Properties["name"])
}

// ---------------------------------------------------------------------------
// ModelIdentifier
// ---------------------------------------------------------------------------

func TestModelIdentifierDetection(t *testing.T) {
	obj := &Object{
		ClassName: ModelIdentifierClass,
		Properties: map[string]any{
			"class":           "App\\Models\\User",
			"id":              int64(42),
			"relations":       &Array{Keys: []any{}, Values: []any{}},
			"connection":      "mysql",
			"collectionClass": nil,
		},
	}
	assert.True(t, IsModelIdentifier(obj))

	mid := AsModelIdentifier(obj)
	require.NotNil(t, mid)
	assert.Equal(t, "App\\Models\\User", mid.Class)
	assert.Equal(t, int64(42), mid.ID)
	assert.Equal(t, "mysql", mid.Connection)
	assert.Empty(t, mid.Relations)
	assert.Nil(t, mid.CollectionClass)
}

func TestModelIdentifierWithRelations(t *testing.T) {
	obj := &Object{
		ClassName: ModelIdentifierClass,
		Properties: map[string]any{
			"class": "App\\Models\\Order",
			"id":    int64(7),
			"relations": &Array{
				Keys:   []any{int64(0), int64(1)},
				Values: []any{"items", "customer"},
			},
			"connection":      "pgsql",
			"collectionClass": nil,
		},
	}
	mid := AsModelIdentifier(obj)
	require.NotNil(t, mid)
	assert.Equal(t, []string{"items", "customer"}, mid.Relations)
}

func TestModelIdentifierToObjectRoundTrip(t *testing.T) {
	cc := `Illuminate\Database\Eloquent\Collection`
	mid := &ModelIdentifier{
		Class:           "App\\Models\\Post",
		ID:              int64(10),
		Relations:       []string{"author"},
		Connection:      "sqlite",
		CollectionClass: &cc,
	}
	obj := mid.ToObject()
	assert.Equal(t, ModelIdentifierClass, obj.ClassName)

	mid2 := AsModelIdentifier(obj)
	require.NotNil(t, mid2)
	assert.Equal(t, mid.Class, mid2.Class)
	assert.Equal(t, mid.ID, mid2.ID)
	assert.Equal(t, mid.Connection, mid2.Connection)
	assert.Equal(t, mid.Relations, mid2.Relations)
	require.NotNil(t, mid2.CollectionClass)
	assert.Equal(t, cc, *mid2.CollectionClass)
}

func TestIsModelIdentifierNonMatch(t *testing.T) {
	obj := &Object{ClassName: "SomeOtherClass", Properties: map[string]any{}}
	assert.False(t, IsModelIdentifier(obj))
	assert.Nil(t, AsModelIdentifier(obj))
}

func TestAsModelIdentifierNonObject(t *testing.T) {
	assert.Nil(t, AsModelIdentifier("not an object"))
	assert.Nil(t, AsModelIdentifier(nil))
}

// ---------------------------------------------------------------------------
// Laravel job payload fixture
// ---------------------------------------------------------------------------

func TestDecodeLaravelJobCommand(t *testing.T) {
	// PHP serialize() uses single backslashes in class names.
	// "App\Jobs\ProcessReport" is 22 bytes.
	data := "O:22:\"App\\Jobs\\ProcessReport\":3:{s:6:\"userId\";i:42;s:8:\"reportId\";s:10:\"rpt_abc123\";s:5:\"queue\";s:7:\"default\";}"
	val, err := Decode(data)
	require.NoError(t, err)
	obj, ok := val.(*Object)
	require.True(t, ok)
	assert.Equal(t, `App\Jobs\ProcessReport`, obj.ClassName)
	assert.Equal(t, int64(42), obj.Properties["userId"])
	assert.Equal(t, "rpt_abc123", obj.Properties["reportId"])
	assert.Equal(t, "default", obj.Properties["queue"])
}

// ---------------------------------------------------------------------------
// Helper functions
// ---------------------------------------------------------------------------

func TestGetString(t *testing.T) {
	obj := &Object{Properties: map[string]any{"k": "v"}}
	assert.Equal(t, "v", GetString(obj, "k"))
	assert.Equal(t, "", GetString(obj, "missing"))
	assert.Equal(t, "", GetString(nil, "k"))
}

func TestGetInt(t *testing.T) {
	obj := &Object{Properties: map[string]any{"n": int64(5), "f": float64(3.9)}}
	assert.Equal(t, int64(5), GetInt(obj, "n"))
	assert.Equal(t, int64(3), GetInt(obj, "f"))
	assert.Equal(t, int64(0), GetInt(obj, "missing"))
	assert.Equal(t, int64(0), GetInt(nil, "n"))
}

func TestGetFloat(t *testing.T) {
	obj := &Object{Properties: map[string]any{"f": float64(1.5), "n": int64(2)}}
	assert.InDelta(t, 1.5, GetFloat(obj, "f"), 1e-9)
	assert.InDelta(t, 2.0, GetFloat(obj, "n"), 1e-9)
	assert.InDelta(t, 0.0, GetFloat(nil, "f"), 1e-9)
}

func TestGetBool(t *testing.T) {
	obj := &Object{Properties: map[string]any{"b": true}}
	assert.True(t, GetBool(obj, "b"))
	assert.False(t, GetBool(obj, "missing"))
	assert.False(t, GetBool(nil, "b"))
}

func TestGetSlice(t *testing.T) {
	arr := &Array{Keys: []any{int64(0)}, Values: []any{"x"}}
	obj := &Object{Properties: map[string]any{"s": arr}}
	assert.Equal(t, []any{"x"}, GetSlice(obj, "s"))
	assert.Nil(t, GetSlice(obj, "missing"))
	assert.Nil(t, GetSlice(nil, "s"))
}

func TestGetMap(t *testing.T) {
	arr := &Array{Keys: []any{"a"}, Values: []any{int64(1)}}
	obj := &Object{Properties: map[string]any{"m": arr}}
	m := GetMap(obj, "m")
	assert.Equal(t, int64(1), m["a"])
	assert.Nil(t, GetMap(obj, "missing"))
	assert.Nil(t, GetMap(nil, "m"))
}

// ---------------------------------------------------------------------------
// Error cases
// ---------------------------------------------------------------------------

func TestDecodeInvalidData(t *testing.T) {
	_, err := Decode("")
	assert.Error(t, err)

	_, err = Decode("x:1;")
	assert.Error(t, err)
}
