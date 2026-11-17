package phpserialize

import (
	"errors"
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

// MaxDepth is the maximum nesting depth Decode accepts. It matches PHP's
// default unserialize_max_depth and keeps hostile payloads from exhausting
// the stack.
const MaxDepth = 4096

type decoder struct {
	data  string
	pos   int
	depth int
	// refs mirrors PHP's unserialize slot table, which r:/R: indexes into
	// (1-based). Every value occupies a slot except array keys, property
	// names and R: references themselves.
	refs []any
}

func (d *decoder) readValue() (any, error) {
	if d.pos >= len(d.data) {
		return nil, errors.New("unexpected end of data")
	}

	d.depth++
	defer func() { d.depth-- }()
	if d.depth > MaxDepth {
		return nil, fmt.Errorf("maximum nesting depth of %d exceeded", MaxDepth)
	}

	indicator := d.data[d.pos]
	if indicator == 'R' {
		// PHP references alias an existing slot and do not create one.
		return d.readReference()
	}

	slot := len(d.refs)
	d.refs = append(d.refs, nil)

	var (
		val any
		err error
	)
	switch indicator {
	case 'N':
		val, err = d.readNull()
	case 'b':
		val, err = d.readBool()
	case 'i':
		val, err = d.readInt()
	case 'd':
		val, err = d.readFloat()
	case 's':
		val, err = d.readString()
	case 'a':
		val, err = d.readArray(slot)
	case 'O':
		val, err = d.readObject(slot)
	case 'r':
		// Object back-references occupy a slot of their own.
		val, err = d.readReference()
	case 'C':
		val, err = d.readCustomObject()
	case 'E':
		val, err = d.readEnum()
	default:
		return nil, fmt.Errorf("unknown type indicator '%c'", indicator)
	}
	if err != nil {
		return nil, err
	}
	d.refs[slot] = val
	return val, nil
}

func (d *decoder) readNull() (any, error) {
	if !d.expect("N;") {
		return nil, fmt.Errorf("expected N;")
	}
	return nil, nil
}

func (d *decoder) readBool() (any, error) {
	if !d.expect("b:") {
		return nil, fmt.Errorf("expected 'b:' prefix")
	}
	if d.pos >= len(d.data) {
		return nil, fmt.Errorf("unexpected end reading bool")
	}
	if d.data[d.pos] != '0' && d.data[d.pos] != '1' {
		return nil, fmt.Errorf("invalid bool value '%c'", d.data[d.pos])
	}
	val := d.data[d.pos] == '1'
	d.pos++
	if !d.expect(";") {
		return nil, fmt.Errorf("expected ; after bool")
	}
	return val, nil
}

func (d *decoder) readInt() (any, error) {
	if !d.expect("i:") {
		return nil, fmt.Errorf("expected 'i:' prefix")
	}
	numStr, err := d.readTerminated()
	if err != nil {
		return nil, err
	}
	val, perr := strconv.ParseInt(numStr, 10, 64)
	if perr != nil {
		return nil, fmt.Errorf("invalid int: %w", perr)
	}
	return val, nil
}

func (d *decoder) readFloat() (any, error) {
	if !d.expect("d:") {
		return nil, fmt.Errorf("expected 'd:' prefix")
	}
	numStr, err := d.readTerminated()
	if err != nil {
		return nil, err
	}

	switch strings.ToUpper(numStr) {
	case "INF":
		return math.Inf(1), nil
	case "-INF":
		return math.Inf(-1), nil
	case "NAN":
		return math.NaN(), nil
	}

	val, perr := strconv.ParseFloat(numStr, 64)
	if perr != nil {
		return nil, fmt.Errorf("invalid float: %w", perr)
	}
	return val, nil
}

func (d *decoder) readString() (string, error) {
	if !d.expect("s:") {
		return "", fmt.Errorf("expected 's:' prefix")
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
	return val, nil
}

func (d *decoder) readArray(slot int) (any, error) {
	if !d.expect("a:") {
		return nil, fmt.Errorf("expected 'a:' prefix")
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
	d.refs[slot] = arr

	for i := range count {
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

func (d *decoder) readObject(slot int) (any, error) {
	if !d.expect("O:") {
		return nil, fmt.Errorf("expected 'O:' prefix")
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
	d.refs[slot] = obj

	for i := range propCount {
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
	numStr, err := d.readTerminated()
	if err != nil {
		return nil, err
	}
	idx, perr := strconv.Atoi(numStr)
	if perr != nil {
		return nil, fmt.Errorf("invalid reference index: %w", perr)
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
		return nil, fmt.Errorf("expected 'C:' prefix")
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
	return obj, nil
}

func (d *decoder) readEnum() (any, error) {
	// PHP 8.1 backed enum: E:{length}:"{ClassName:CaseName}";
	if !d.expect("E:") {
		return nil, fmt.Errorf("expected 'E:' prefix")
	}
	length, err := d.readLength()
	if err != nil {
		return nil, err
	}
	if !d.expect(`:"`) {
		return nil, fmt.Errorf("expected :\" after enum length")
	}
	if d.pos+length > len(d.data) {
		return nil, fmt.Errorf("enum length %d exceeds data", length)
	}
	val := d.data[d.pos : d.pos+length]
	d.pos += length
	if !d.expect(`";`) {
		return nil, fmt.Errorf("expected \"; after enum value")
	}

	// Split "ClassName:CaseName" on the last colon to handle namespaced class names.
	idx := strings.LastIndex(val, ":")
	if idx < 0 {
		return nil, fmt.Errorf("invalid enum value %q: missing colon separator", val)
	}

	enum := &Enum{
		ClassName: val[:idx],
		CaseName:  val[idx+1:],
	}
	return enum, nil
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
	// Every counted byte or element needs at least one byte of input, so a
	// length beyond what is left is malformed. Rejecting it here also stops a
	// forged count from driving a huge allocation.
	if val < 0 || val > len(d.data)-d.pos {
		return 0, fmt.Errorf("length %d out of range", val)
	}
	return val, nil
}

// readTerminated reads up to the next ';' and consumes it.
func (d *decoder) readTerminated() (string, error) {
	s := d.readUntil(';')
	if d.pos >= len(d.data) {
		return "", errors.New("missing ; terminator")
	}
	d.pos++
	return s, nil
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
