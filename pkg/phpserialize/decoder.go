package phpserialize

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

func Decode(data string) (any, error) {
	d := &decoder{data: data, pos: 0}
	val, err := d.readValue()
	if err != nil {
		return nil, fmt.Errorf("phpserialize: decode error at pos %d: %w", d.pos, err)
	}
	return val, nil
}

func DecodeBytes(data []byte) (any, error) {
	return Decode(string(data))
}

type decoder struct {
	data string
	pos  int
	refs []any // for reference tracking
}

func (d *decoder) readValue() (any, error) {
	if d.pos >= len(d.data) {
		return nil, fmt.Errorf("unexpected end of data")
	}

	switch d.data[d.pos] {
	case 'N':
		return d.readNull()
	case 'b':
		return d.readBool()
	case 'i':
		return d.readInt()
	case 'd':
		return d.readFloat()
	case 's':
		return d.readString()
	case 'a':
		return d.readArray()
	case 'O':
		return d.readObject()
	case 'r', 'R':
		return d.readReference()
	case 'C':
		return d.readCustomObject()
	default:
		return nil, fmt.Errorf("unknown type indicator '%c'", d.data[d.pos])
	}
}

func (d *decoder) readNull() (any, error) {
	if !d.expect("N;") {
		return nil, fmt.Errorf("expected N;")
	}
	d.refs = append(d.refs, nil)
	return nil, nil
}

func (d *decoder) readBool() (any, error) {
	if !d.expect("b:") {
		return nil, fmt.Errorf("expected b:")
	}
	if d.pos >= len(d.data) {
		return nil, fmt.Errorf("unexpected end reading bool")
	}
	val := d.data[d.pos] == '1'
	d.pos++
	if !d.expect(";") {
		return nil, fmt.Errorf("expected ; after bool")
	}
	d.refs = append(d.refs, val)
	return val, nil
}

func (d *decoder) readInt() (any, error) {
	if !d.expect("i:") {
		return nil, fmt.Errorf("expected i:")
	}
	numStr := d.readUntil(';')
	d.pos++ // skip ;
	val, err := strconv.ParseInt(numStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid int: %w", err)
	}
	d.refs = append(d.refs, val)
	return val, nil
}

func (d *decoder) readFloat() (any, error) {
	if !d.expect("d:") {
		return nil, fmt.Errorf("expected d:")
	}
	numStr := d.readUntil(';')
	d.pos++ // skip ;

	switch strings.ToUpper(numStr) {
	case "INF":
		d.refs = append(d.refs, math.Inf(1))
		return math.Inf(1), nil
	case "-INF":
		d.refs = append(d.refs, math.Inf(-1))
		return math.Inf(-1), nil
	case "NAN":
		d.refs = append(d.refs, math.NaN())
		return math.NaN(), nil
	}

	val, err := strconv.ParseFloat(numStr, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid float: %w", err)
	}
	d.refs = append(d.refs, val)
	return val, nil
}

func (d *decoder) readString() (string, error) {
	if !d.expect("s:") {
		return "", fmt.Errorf("expected s:")
	}
	length, err := d.readLength()
	if err != nil {
		return "", err
	}
	if !d.expect(`:"`) {
		return "", fmt.Errorf("expected :\" after string length")
	}
	if d.pos+length > len(d.data) {
		return "", fmt.Errorf("string length %d exceeds data", length)
	}
	val := d.data[d.pos : d.pos+length]
	d.pos += length
	if !d.expect(`";`) {
		return "", fmt.Errorf("expected \"; after string content")
	}
	d.refs = append(d.refs, val)
	return val, nil
}

func (d *decoder) readArray() (any, error) {
	if !d.expect("a:") {
		return nil, fmt.Errorf("expected a:")
	}
	count, err := d.readLength()
	if err != nil {
		return nil, err
	}
	if !d.expect(":{") {
		return nil, fmt.Errorf("expected :{ after array count")
	}

	arr := &Array{
		Keys:   make([]any, 0, count),
		Values: make([]any, 0, count),
	}
	d.refs = append(d.refs, arr)

	for i := 0; i < count; i++ {
		key, err := d.readArrayKey()
		if err != nil {
			return nil, fmt.Errorf("reading array key %d: %w", i, err)
		}
		val, err := d.readValue()
		if err != nil {
			return nil, fmt.Errorf("reading array value %d: %w", i, err)
		}
		arr.Keys = append(arr.Keys, key)
		arr.Values = append(arr.Values, val)
	}

	if !d.expect("}") {
		return nil, fmt.Errorf("expected } after array")
	}

	return arr, nil
}

func (d *decoder) readArrayKey() (any, error) {
	if d.pos >= len(d.data) {
		return nil, fmt.Errorf("unexpected end reading array key")
	}
	switch d.data[d.pos] {
	case 'i':
		return d.readInt()
	case 's':
		return d.readString()
	default:
		return nil, fmt.Errorf("unexpected array key type '%c'", d.data[d.pos])
	}
}

func (d *decoder) readObject() (any, error) {
	if !d.expect("O:") {
		return nil, fmt.Errorf("expected O:")
	}
	classNameLen, err := d.readLength()
	if err != nil {
		return nil, err
	}
	if !d.expect(`:"`) {
		return nil, fmt.Errorf("expected :\" after class name length")
	}
	if d.pos+classNameLen > len(d.data) {
		return nil, fmt.Errorf("class name length exceeds data")
	}
	className := d.data[d.pos : d.pos+classNameLen]
	d.pos += classNameLen
	if !d.expect(`":`) {
		return nil, fmt.Errorf("expected \": after class name")
	}

	propCount, err := d.readLength()
	if err != nil {
		return nil, err
	}
	if !d.expect(":{") {
		return nil, fmt.Errorf("expected :{ after property count")
	}

	obj := &Object{
		ClassName:  className,
		Properties: make(map[string]any, propCount),
	}
	d.refs = append(d.refs, obj)

	for i := 0; i < propCount; i++ {
		rawKey, err := d.readString()
		if err != nil {
			return nil, fmt.Errorf("reading property key %d: %w", i, err)
		}
		// Strip visibility prefixes from property names.
		cleanKey := stripVisibility(rawKey)
		val, err := d.readValue()
		if err != nil {
			return nil, fmt.Errorf("reading property value for '%s': %w", cleanKey, err)
		}
		obj.Properties[cleanKey] = val
	}

	if !d.expect("}") {
		return nil, fmt.Errorf("expected } after object")
	}

	return obj, nil
}

func (d *decoder) readReference() (any, error) {
	d.pos++ // skip r or R
	if !d.expect(":") {
		return nil, fmt.Errorf("expected : after reference type")
	}
	numStr := d.readUntil(';')
	d.pos++ // skip ;
	idx, err := strconv.Atoi(numStr)
	if err != nil {
		return nil, fmt.Errorf("invalid reference index: %w", err)
	}
	// PHP references are 1-based.
	idx--
	if idx < 0 || idx >= len(d.refs) {
		return nil, fmt.Errorf("reference index %d out of range", idx+1)
	}
	return d.refs[idx], nil
}

func (d *decoder) readCustomObject() (any, error) {
	// C:className:dataLen:{data} — custom serializable objects.
	// We treat these as opaque strings since we can't deserialize custom PHP logic.
	if !d.expect("C:") {
		return nil, fmt.Errorf("expected C:")
	}
	classNameLen, err := d.readLength()
	if err != nil {
		return nil, err
	}
	if !d.expect(`:"`) {
		return nil, fmt.Errorf("expected :\" after class name length")
	}
	if d.pos+classNameLen > len(d.data) {
		return nil, fmt.Errorf("custom object class name length exceeds data")
	}
	className := d.data[d.pos : d.pos+classNameLen]
	d.pos += classNameLen
	if !d.expect(`":`) {
		return nil, fmt.Errorf("expected \": after class name")
	}
	dataLen, err := d.readLength()
	if err != nil {
		return nil, err
	}
	if !d.expect(":{") {
		return nil, fmt.Errorf("expected :{ after data length")
	}
	if d.pos+dataLen > len(d.data) {
		return nil, fmt.Errorf("custom object data exceeds buffer")
	}
	d.pos += dataLen
	if !d.expect("}") {
		return nil, fmt.Errorf("expected } after custom object")
	}

	obj := &Object{
		ClassName:  className,
		Properties: map[string]any{},
	}
	d.refs = append(d.refs, obj)
	return obj, nil
}

func (d *decoder) readLength() (int, error) {
	numStr := d.readUntil(':')
	if numStr == "" {
		return 0, fmt.Errorf("empty length")
	}
	val, err := strconv.Atoi(numStr)
	if err != nil {
		return 0, fmt.Errorf("invalid length: %w", err)
	}
	return val, nil
}

func (d *decoder) expect(s string) bool {
	if d.pos+len(s) > len(d.data) {
		return false
	}
	if d.data[d.pos:d.pos+len(s)] == s {
		d.pos += len(s)
		return true
	}
	return false
}

func (d *decoder) readUntil(ch byte) string {
	start := d.pos
	for d.pos < len(d.data) && d.data[d.pos] != ch {
		d.pos++
	}
	return d.data[start:d.pos]
}

// stripVisibility removes PHP visibility prefixes from property names.
// Public: "name" → "name"
// Protected: "\x00*\x00name" → "name"
// Private: "\x00ClassName\x00name" → "name"
func stripVisibility(name string) string {
	if len(name) == 0 || name[0] != '\x00' {
		return name
	}
	// Find the second null byte.
	idx := strings.IndexByte(name[1:], '\x00')
	if idx < 0 {
		return name
	}
	return name[idx+2:]
}

func ParsePropertyName(raw string) PropertyInfo {
	if len(raw) == 0 || raw[0] != '\x00' {
		return PropertyInfo{Name: raw, Visibility: Public}
	}
	if len(raw) >= 3 && raw[1] == '*' && raw[2] == '\x00' {
		return PropertyInfo{Name: raw[3:], Visibility: Protected}
	}
	idx := strings.IndexByte(raw[1:], '\x00')
	if idx < 0 {
		return PropertyInfo{Name: raw, Visibility: Public}
	}
	return PropertyInfo{
		Name:       raw[idx+2:],
		Visibility: Private,
		ClassName:  raw[1 : idx+1],
	}
}
