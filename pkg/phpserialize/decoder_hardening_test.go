package phpserialize

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The serialized strings in these tests were produced by PHP 8's serialize().

func TestDecodeObjectBackReference(t *testing.T) {
	// $b = new stdClass; $b->v = 1;
	// $a = new A; $a->o = $b; $a->p = $b; $a->arr = ['k' => 'v', 'z' => $b]; $a->q = &$a->name;
	const data = `O:1:"A":5:{s:4:"name";s:1:"x";s:1:"o";O:8:"stdClass":1:{s:1:"v";i:1;}s:1:"p";r:3;s:3:"arr";a:2:{s:1:"k";s:1:"v";s:1:"z";r:3;}s:1:"q";R:2;}`

	decoded, err := Decode(data)
	require.NoError(t, err)
	obj, ok := decoded.(*Object)
	require.True(t, ok)

	shared, ok := obj.Properties["o"].(*Object)
	require.True(t, ok)
	assert.Equal(t, "stdClass", shared.ClassName)

	assert.Same(t, shared, obj.Properties["p"], "r:3 must resolve to the stdClass in slot 3")
	arr, ok := obj.Properties["arr"].(*Array)
	require.True(t, ok)
	assert.Same(t, shared, arr.Values[1], "array keys must not occupy reference slots")
	assert.Equal(t, "x", obj.Properties["q"], "R:2 must resolve to the name property")
}

func TestDecodeBackReferenceOccupiesSlot(t *testing.T) {
	// r: takes a slot of its own, R: does not: slots are 1=array, 2=object,
	// 3=r:2, 4="x", so R:4 is "x".
	decoded, err := Decode(`a:4:{i:0;O:8:"stdClass":0:{}i:1;r:2;i:2;s:1:"x";i:3;R:4;}`)
	require.NoError(t, err)
	arr, ok := decoded.(*Array)
	require.True(t, ok)
	assert.Same(t, arr.Values[0], arr.Values[1])
	assert.Equal(t, "x", arr.Values[3])

	// PHP rejects this one: R:2 creates no slot, so there is no slot 3.
	_, err = Decode(`a:3:{i:0;s:1:"y";i:1;R:2;i:2;R:3;}`)
	assert.Error(t, err)
}

func TestDecodeSelfReference(t *testing.T) {
	decoded, err := Decode(`O:1:"A":1:{s:4:"self";r:1;}`)
	require.NoError(t, err)
	obj, ok := decoded.(*Object)
	require.True(t, ok)
	assert.Same(t, obj, obj.Properties["self"])
}

func TestDecodeRejectsMalformedInput(t *testing.T) {
	cases := map[string]string{
		"negative string length": `s:-1:"";`,
		"negative array count":   `a:-1:{}`,
		"negative class length":  `O:-1:"A":0:{}`,
		"forged array count":     `a:999999999999:{}`,
		"forged property count":  `O:1:"A":999999999999:{}`,
		"forged string length":   `s:999999999999:"x";`,
		"forged custom length":   `C:1:"A":999999999999:{}`,
		"invalid bool":           `b:2;`,
		"unterminated int":       `i:1`,
		"unterminated float":     `d:1.5`,
		"unterminated reference": `a:1:{i:0;r:1`,
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			assert.NotPanics(t, func() {
				_, err := Decode(data)
				assert.Error(t, err)
			})
		})
	}
}

func TestDecodeDepthLimit(t *testing.T) {
	nest := func(depth int) string {
		return strings.Repeat(`a:1:{i:0;`, depth) + `N;` + strings.Repeat(`}`, depth)
	}

	_, err := Decode(nest(MaxDepth - 1))
	require.NoError(t, err)

	_, err = Decode(nest(MaxDepth + 1))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "maximum nesting depth")
}

func FuzzDecode(f *testing.F) {
	for _, seed := range []string{
		`N;`, `b:1;`, `i:42;`, `d:0.1;`, `s:1:"x";`,
		`a:2:{i:0;s:1:"a";s:1:"k";i:1;}`,
		`O:1:"A":1:{s:4:"self";r:1;}`,
		`C:1:"A":2:{ab}`,
		`E:11:"App\S:Active";`,
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data string) {
		_, _ = Decode(data) // must never panic
	})
}
