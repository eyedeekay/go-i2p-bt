package bencode

import (
	"bufio"
	"bytes"
	"encoding"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strconv"
	"strings"
)

var (
	reflectByteSliceType = reflect.TypeOf([]byte(nil))
	reflectStringType    = reflect.TypeOf("")
)

// Unmarshaler is the interface implemented by types that can unmarshal
// a bencode description of themselves.
// The input can be assumed to be a valid encoding of a bencode value.
// UnmarshalBencode must copy the bencode data if it wishes to retain the data after returning.
type Unmarshaler interface {
	UnmarshalBencode([]byte) error
}

// A Decoder reads and decodes bencoded data from an input stream.
type Decoder struct {
	r             *bufio.Reader
	raw           bool
	buf           []byte
	n             int
	failUnordered bool
}

// SetFailOnUnorderedKeys will cause the decoder to fail when encountering
// unordered keys. The default is to not fail.
func (d *Decoder) SetFailOnUnorderedKeys(fail bool) {
	d.failUnordered = fail
}

// BytesParsed returns the number of bytes that have actually been parsed
func (d *Decoder) BytesParsed() int {
	return d.n
}

// read also writes into the buffer when d.raw is set.
func (d *Decoder) read(p []byte) (n int, err error) {
	n, err = d.r.Read(p)
	if d.raw {
		d.buf = append(d.buf, p[:n]...)
	}
	d.n += n
	return
}

// readBytes also writes into the buffer when d.raw is set.
func (d *Decoder) readBytes(delim byte) (line []byte, err error) {
	line, err = d.r.ReadBytes(delim)
	if d.raw {
		d.buf = append(d.buf, line...)
	}
	d.n += len(line)
	return
}

// readByte also writes into the buffer when d.raw is set.
func (d *Decoder) readByte() (b byte, err error) {
	b, err = d.r.ReadByte()
	if d.raw {
		d.buf = append(d.buf, b)
	}
	d.n++
	return
}

// readFull also writes into the buffer when d.raw is set.
func (d *Decoder) readFull(p []byte) (n int, err error) {
	n, err = io.ReadFull(d.r, p)
	if d.raw {
		d.buf = append(d.buf, p[:n]...)
	}
	d.n += n
	return
}

func (d *Decoder) peekByte() (b byte, err error) {
	ch, err := d.r.Peek(1)
	if err != nil {
		return
	}
	b = ch[0]
	return
}

// NewDecoder returns a new decoder that reads from r
func NewDecoder(r io.Reader) *Decoder {
	return &Decoder{r: bufio.NewReader(r)}
}

// Decode reads the bencoded value from its input and stores it in the value pointed to by val.
// Decode allocates maps/slices as necessary with the following additional rules:
// To decode a bencoded value into a nil interface value, the type stored in the interface value is one of:
//
//	int64 for bencoded integers
//	string for bencoded strings
//	[]interface{} for bencoded lists
//	map[string]interface{} for bencoded dicts
//
// To unmarshal bencode into a value implementing the Unmarshaler interface,
// Unmarshal calls that value's UnmarshalBencode method.
// Otherwise, if the value implements encoding.TextUnmarshaler
// and the input is a bencode string, Unmarshal calls that value's
// UnmarshalText method with the decoded form of the string.
func (d *Decoder) Decode(val interface{}) error {
	rv := reflect.ValueOf(val)
	if rv.Kind() != reflect.Ptr || rv.IsNil() {
		return errors.New("Unwritable type passed into decode")
	}
	//log.Printf("Decoding %s", rv)
	return d.decodeInto(rv)
}

// DecodeString reads the data in the string and stores it into the value pointed to by val.
// Read the docs for Decode for more information.
func DecodeString(in string, val interface{}) error {
	buf := strings.NewReader(in)
	d := NewDecoder(buf)
	return d.Decode(val)
}

// DecodeBytes reads the data in b and stores it into the value pointed to by val.
// Read the docs for Decode for more information.
func DecodeBytes(b []byte, val interface{}) error {
	r := bytes.NewReader(b)
	d := NewDecoder(r)
	return d.Decode(val)
}

func indirect(v reflect.Value, alloc bool) reflect.Value {
	for {
		switch v.Kind() {
		case reflect.Interface:
			if v.IsNil() {
				if !alloc {
					return reflect.Value{}
				}
				return v
			}

		case reflect.Ptr:
			if v.IsNil() {
				if !alloc {
					return reflect.Value{}
				}
				v.Set(reflect.New(v.Type().Elem()))
			}

		default:
			return v
		}

		v = v.Elem()
	}
}

// decodeInto decodes a bencode value into the provided reflect.Value.
// It handles special unmarshaler interfaces, raw message processing, and
// dispatches to appropriate type-specific decode methods.
func (d *Decoder) decodeInto(val reflect.Value) (err error) {
	var v reflect.Value
	if d.raw {
		v = val
	} else {
		var (
			unmarshaler     Unmarshaler
			textUnmarshaler encoding.TextUnmarshaler
		)
		unmarshaler, textUnmarshaler, v = d.indirect(val)

		// if we're decoding into an Unmarshaler,
		// we pass on the next bencode value to this value instead,
		// so it can decide what to do with it.
		if unmarshaler != nil {
			var x RawMessage
			if err := d.decodeInto(reflect.ValueOf(&x)); err != nil {
				return err
			}
			return unmarshaler.UnmarshalBencode([]byte(x))
		}

		// if we're decoding into an TextUnmarshaler,
		// we'll assume that the bencode value is a string,
		// we decode it as such and pass the result onto the unmarshaler.
		if textUnmarshaler != nil {
			var b []byte
			ref := reflect.ValueOf(&b)
			if err := d.decodeString(reflect.Indirect(ref)); err != nil {
				return err
			}
			return textUnmarshaler.UnmarshalText(b)
		}

		// if we're decoding into a RawMessage set raw to true for the rest of
		// the call stack, and switch out the value with an interface{}.
		if _, ok := v.Interface().(RawMessage); ok {
			v = reflect.Value{} // explicitly make v invalid

			// set d.raw for the lifetime of this function call, and set the raw
			// message when the function is exiting.
			d.buf = d.buf[:0]
			d.raw = true
			defer func() {
				d.raw = false
				v := indirect(val, true)
				v.SetBytes(append([]byte(nil), d.buf...))
			}()
		}
	}

	return d.dispatchDecodeByType(v)
}

// dispatchDecodeByType examines the next byte and dispatches to the appropriate
// type-specific decode method based on bencode format markers.
func (d *Decoder) dispatchDecodeByType(v reflect.Value) error {
	next, err := d.peekByte()
	if err != nil {
		return err
	}

	switch next {
	case 'i':
		return d.decodeInt(v)
	case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		return d.decodeString(v)
	case 'l':
		return d.decodeList(v)
	case 'd':
		return d.decodeDict(v)
	default:
		return errors.New("invalid input")
	}
}

// decodeInt reads and decodes a bencode integer value into the provided reflect.Value.
// It handles integer token validation, digit extraction, and type-specific assignment
// for all supported integer, unsigned integer, boolean, and interface types.
func (d *Decoder) decodeInt(v reflect.Value) error {
	digits, err := d.readIntegerDigits()
	if err != nil {
		return err
	}

	return d.assignIntegerValue(v, digits)
}

// readIntegerDigits validates the integer token format and extracts the digit string.
// It ensures the input starts with 'i' and ends with 'e', returning the digits between.
func (d *Decoder) readIntegerDigits() (string, error) {
	ch, err := d.readByte()
	if err != nil {
		return "", err
	}
	if ch != 'i' {
		panic("got not an i when peek returned an i")
	}

	line, err := d.readBytes('e')
	if err != nil || d.raw {
		return "", err
	}

	return string(line[:len(line)-1]), nil
}

// assignIntegerValue converts the digit string to the appropriate type and assigns it
// to the reflect.Value based on its kind (interface, signed/unsigned integers, bool).
func (d *Decoder) assignIntegerValue(v reflect.Value, digits string) error {
	if !v.IsValid() {
		// For invalid values (e.g., during raw message processing), no assignment needed
		return nil
	}

	switch v.Kind() {
	default:
		return fmt.Errorf("Cannot store int64 into %s", v.Type())
	case reflect.Interface:
		return d.assignToInterface(v, digits)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return d.assignToSignedInt(v, digits)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return d.assignToUnsignedInt(v, digits)
	case reflect.Bool:
		return d.assignToBool(v, digits)
	}
}

// assignToInterface parses the digits as int64 and assigns to an interface{} value.
func (d *Decoder) assignToInterface(v reflect.Value, digits string) error {
	n, err := strconv.ParseInt(digits, 10, 64)
	if err != nil {
		return err
	}
	v.Set(reflect.ValueOf(n))
	return nil
}

// assignToSignedInt parses the digits as int64 and assigns to signed integer types.
func (d *Decoder) assignToSignedInt(v reflect.Value, digits string) error {
	n, err := strconv.ParseInt(digits, 10, 64)
	if err != nil {
		return err
	}
	v.SetInt(n)
	return nil
}

// assignToUnsignedInt parses the digits as uint64 and assigns to unsigned integer types.
func (d *Decoder) assignToUnsignedInt(v reflect.Value, digits string) error {
	n, err := strconv.ParseUint(digits, 10, 64)
	if err != nil {
		return err
	}
	v.SetUint(n)
	return nil
}

// assignToBool parses the digits as uint64 and assigns the boolean result (non-zero = true).
func (d *Decoder) assignToBool(v reflect.Value, digits string) error {
	n, err := strconv.ParseUint(digits, 10, 64)
	if err != nil {
		return err
	}
	v.SetBool(n != 0)
	return nil
}

func (d *Decoder) decodeString(v reflect.Value) error {
	// read until a colon to get the number of digits to read after
	line, err := d.readBytes(':')
	if err != nil {
		return err
	}

	// parse it into an int for making a slice
	l32, err := strconv.ParseInt(string(line[:len(line)-1]), 10, 32)
	l := int(l32)
	if err != nil {
		return err
	}
	if l < 0 {
		return fmt.Errorf("invalid negative string length: %d", l)
	}

	// read exactly l bytes out and make our string
	buf := make([]byte, l)
	_, err = d.readFull(buf)
	if err != nil || d.raw {
		return err
	}

	switch v.Kind() {
	default:
		return fmt.Errorf("Cannot store string into %s", v.Type())
	case reflect.Slice:
		if v.Type() != reflectByteSliceType {
			return fmt.Errorf("Cannot store string into %s", v.Type())
		}
		v.SetBytes(buf)
	case reflect.String:
		v.SetString(string(buf))
	case reflect.Interface:
		v.Set(reflect.ValueOf(string(buf)))
	}
	return nil
}

func (d *Decoder) decodeList(v reflect.Value) error {
	if !d.raw {
		// if we have an interface, just put a []interface{} in it!
		if v.Kind() == reflect.Interface {
			var x []interface{}
			defer func(p reflect.Value) { p.Set(reflect.ValueOf(x)) }(v)
			v = reflect.ValueOf(&x).Elem()
		}

		if v.Kind() != reflect.Array && v.Kind() != reflect.Slice {
			return fmt.Errorf("Cant store a []interface{} into %s", v.Type())
		}
	}

	if err := d.consumeListToken(); err != nil {
		return err
	}

	if d.raw {
		return d.processListInRawMode(v)
	}

	return d.processListElements(v)
}

// consumeListToken reads and validates the list start token 'l'.
func (d *Decoder) consumeListToken() error {
	ch, err := d.readByte()
	if err != nil {
		return err
	}
	if ch != 'l' {
		panic("got something other than a list head after a peek")
	}
	return nil
}

// processListInRawMode handles list decoding when in raw mode.
func (d *Decoder) processListInRawMode(v reflect.Value) error {
	for {
		// peek for the end token and read it out
		ch, err := d.peekByte()
		if err != nil {
			return err
		}
		if ch == 'e' {
			_, err = d.readByte() // consume the end
			return err
		}

		// decode the next value
		err = d.decodeInto(v)
		if err != nil {
			return err
		}
	}
}

// processListElements processes the main element decoding loop for lists.
func (d *Decoder) processListElements(v reflect.Value) error {
	for i := 0; ; i++ {
		// peek for the end token and read it out
		ch, err := d.peekByte()
		if err != nil {
			return err
		}
		switch ch {
		case 'e':
			_, err := d.readByte() // consume the end
			return err
		}

		if err := d.ensureSliceCapacity(&v, i); err != nil {
			return err
		}

		// decode a value into the index
		if err := d.decodeInto(v.Index(i)); err != nil {
			return err
		}
	}
}

// ensureSliceCapacity grows the slice capacity if needed and adjusts length.
func (d *Decoder) ensureSliceCapacity(v *reflect.Value, index int) error {
	// grow it if required
	if index >= v.Cap() && v.IsValid() {
		newcap := v.Cap() + v.Cap()/2
		if newcap < 4 {
			newcap = 4
		}
		newv := reflect.MakeSlice(v.Type(), v.Len(), newcap)
		reflect.Copy(newv, *v)
		v.Set(newv)
	}

	// reslice into cap (its a slice now since it had to have grown)
	if index >= v.Len() && v.IsValid() {
		v.SetLen(index + 1)
	}
	return nil
}

func (d *Decoder) decodeDict(v reflect.Value) error {
	// if we have an interface{}, just put a map[string]interface{} in it!
	if !d.raw && v.Kind() == reflect.Interface {
		var x map[string]interface{}
		defer func(p reflect.Value) { p.Set(reflect.ValueOf(x)) }(v)
		v = reflect.ValueOf(&x).Elem()
	}

	if err := d.consumeDictToken(); err != nil {
		return err
	}

	if d.raw {
		return d.processDictInRawMode(v)
	}

	mapElem, isMap, vals, err := d.setupDictTarget(v)
	if err != nil {
		return err
	}

	return d.processDictKeyValuePairs(v, mapElem, isMap, vals)
}

// consumeDictToken reads and validates the dictionary start token 'd'.
func (d *Decoder) consumeDictToken() error {
	ch, err := d.readByte()
	if err != nil {
		return err
	}
	if ch != 'd' {
		panic("got an incorrect token when it was checked already")
	}
	return nil
}

// processDictInRawMode handles dictionary decoding when in raw mode.
func (d *Decoder) processDictInRawMode(v reflect.Value) error {
	for {
		ch, err := d.peekByte()
		if err != nil {
			return err
		}
		if ch == 'e' {
			_, err = d.readByte() // consume the end token
			return err
		}

		err = d.decodeString(v)
		if err != nil {
			return err
		}

		err = d.decodeInto(v)
		if err != nil {
			return err
		}
	}
}

// setupDictTarget validates the target type and prepares variables for dictionary decoding.
func (d *Decoder) setupDictTarget(v reflect.Value) (mapElem reflect.Value, isMap bool, vals map[string]reflect.Value, err error) {
	switch v.Kind() {
	case reflect.Map:
		t := v.Type()
		if t.Key() != reflectStringType {
			return reflect.Value{}, false, nil, fmt.Errorf("Can't store a map[string]interface{} into %s", v.Type())
		}
		if v.IsNil() {
			v.Set(reflect.MakeMap(t))
		}

		isMap = true
		mapElem = reflect.New(t.Elem()).Elem()
	case reflect.Struct:
		vals = make(map[string]reflect.Value)
		setStructValues(vals, v)
	default:
		return reflect.Value{}, false, nil, fmt.Errorf("Can't store a map[string]interface{} into %s", v.Type())
	}
	return mapElem, isMap, vals, nil
}

// processDictKeyValuePairs processes the main key-value parsing loop for dictionaries.
func (d *Decoder) processDictKeyValuePairs(v, mapElem reflect.Value, isMap bool, vals map[string]reflect.Value) error {
	var lastKey string
	first := true

	for {
		if isDictEnd, err := d.checkDictEnd(); err != nil {
			return err
		} else if isDictEnd {
			return nil
		}

		key, err := d.processNextDictKey(&lastKey, &first)
		if err != nil {
			return err
		}

		if err := d.handleKeyValuePair(key, v, mapElem, isMap, vals); err != nil {
			return err
		}
	}
}

// checkDictEnd checks if the dictionary has reached its end marker.
func (d *Decoder) checkDictEnd() (bool, error) {
	ch, err := d.peekByte()
	if err != nil {
		return false, err
	}
	if ch == 'e' {
		_, err = d.readByte() // consume the end token
		return true, err
	}
	return false, nil
}

// processNextDictKey reads and validates the next dictionary key.
func (d *Decoder) processNextDictKey(lastKey *string, first *bool) (string, error) {
	key, err := d.readDictKey()
	if err != nil {
		return "", err
	}

	if err := d.validateKeyOrder(lastKey, first, key); err != nil {
		return "", err
	}

	return key, nil
}

// handleKeyValuePair processes a complete key-value pair in the dictionary.
func (d *Decoder) handleKeyValuePair(key string, v, mapElem reflect.Value, isMap bool, vals map[string]reflect.Value) error {
	subv, err := d.prepareValueTarget(key, mapElem, isMap, vals, v)
	if err != nil {
		return err
	}

	if !subv.IsValid() {
		return d.skipInvalidValue()
	}

	if err := d.decodeInto(subv); err != nil {
		return err
	}

	if isMap {
		v.SetMapIndex(reflect.ValueOf(key), subv)
	}
	return nil
}

// readDictKey reads and returns the next dictionary key.
func (d *Decoder) readDictKey() (string, error) {
	var key string
	if err := d.decodeString(reflect.ValueOf(&key).Elem()); err != nil {
		return "", err
	}
	return key, nil
}

// validateKeyOrder checks for unordered keys if required.
func (d *Decoder) validateKeyOrder(lastKey *string, first *bool, key string) error {
	if !*first && d.failUnordered && *lastKey > key {
		return fmt.Errorf("unordered dictionary: %q appears before %q", *lastKey, key)
	}
	*lastKey, *first = key, false
	return nil
}

// prepareValueTarget prepares the target value for decoding based on the key and target type.
func (d *Decoder) prepareValueTarget(key string, mapElem reflect.Value, isMap bool, vals map[string]reflect.Value, v reflect.Value) (reflect.Value, error) {
	if isMap {
		mapElem.Set(reflect.Zero(v.Type().Elem()))
		return mapElem, nil
	}
	return vals[key], nil
}

// skipInvalidValue skips a value when the target is invalid.
func (d *Decoder) skipInvalidValue() error {
	var x interface{}
	return d.decodeInto(reflect.ValueOf(&x).Elem())
}

// indirect walks down v allocating pointers as needed,
// until it gets to a non-pointer.
// if it encounters an (Text)Unmarshaler, indirect stops and returns that.
func (d *Decoder) indirect(v reflect.Value) (Unmarshaler, encoding.TextUnmarshaler, reflect.Value) {
	v = d.prepareAddressableValue(v)
	return d.navigateValueChain(v)
}

// prepareAddressableValue ensures that named types are addressable for pointer method access.
// If the value is a named type and can be addressed, it returns the address of the value.
func (d *Decoder) prepareAddressableValue(v reflect.Value) reflect.Value {
	if v.Kind() != reflect.Ptr && v.Type().Name() != "" && v.CanAddr() {
		v = v.Addr()
	}
	return v
}

// navigateValueChain walks through the pointer and interface chain, checking for unmarshalers.
// It returns any unmarshaler interfaces found or the final concrete value.
func (d *Decoder) navigateValueChain(v reflect.Value) (Unmarshaler, encoding.TextUnmarshaler, reflect.Value) {
	for {
		v = d.dereferenceInterface(v)
		
		if v.Kind() != reflect.Ptr || v.IsNil() {
			break
		}

		if unmarshaler, textUnmarshaler := d.checkUnmarshalers(v); unmarshaler != nil || textUnmarshaler != nil {
			return unmarshaler, textUnmarshaler, reflect.Value{}
		}

		v = v.Elem()
	}
	return nil, nil, indirect(v, true)
}

// dereferenceInterface handles interface type unwrapping when the result will be usefully addressable.
// It returns the dereferenced value if it's a non-nil pointer interface, otherwise returns the original value.
func (d *Decoder) dereferenceInterface(v reflect.Value) reflect.Value {
	if v.Kind() == reflect.Interface && !v.IsNil() {
		e := v.Elem()
		if e.Kind() == reflect.Ptr && !e.IsNil() {
			return e
		}
	}
	return v
}

// checkUnmarshalers examines the value's interface to detect Unmarshaler or TextUnmarshaler implementations.
// It returns the appropriate unmarshaler if found, or nil values if none are detected.
func (d *Decoder) checkUnmarshalers(v reflect.Value) (Unmarshaler, encoding.TextUnmarshaler) {
	vi := v.Interface()
	if u, ok := vi.(Unmarshaler); ok {
		return u, nil
	}
	if u, ok := vi.(encoding.TextUnmarshaler); ok {
		return nil, u
	}
	return nil, nil
}

func setStructValues(m map[string]reflect.Value, v reflect.Value) {
	t := v.Type()
	if t.Kind() != reflect.Struct {
		return
	}

	// Process embedded fields first to establish base mapping
	processEmbeddedFields(m, v)

	// Process regular fields, overwriting embedded field mappings as needed
	processRegularFields(m, v)
}

// processEmbeddedFields handles the recursive processing of embedded struct fields.
// It iterates through all fields looking for anonymous embedded structs without tags
// and recursively processes them to build the field mapping.
func processEmbeddedFields(m map[string]reflect.Value, v reflect.Value) {
	for i := 0; i < v.NumField(); i++ {
		f := v.Type().Field(i)
		if f.PkgPath != "" {
			continue
		}
		fieldValue := v.FieldByIndex(f.Index)
		if f.Anonymous && f.Tag == "" {
			setStructValues(m, fieldValue)
		}
	}
}

// processRegularFields processes all struct fields to extract field names and map them
// to their reflection values. It handles both tagged and untagged fields, with tagged
// fields taking precedence over embedded field mappings.
func processRegularFields(m map[string]reflect.Value, v reflect.Value) {
	for i := 0; i < v.NumField(); i++ {
		f := v.Type().Field(i)
		if f.PkgPath != "" {
			continue
		}
		fieldValue := v.FieldByIndex(f.Index)
		name := extractFieldName(f)
		if name != "" && isValidTag(name) {
			m[name] = fieldValue
		}
	}
}

// extractFieldName determines the appropriate field name from struct field information.
// It processes bencode tags and handles anonymous fields according to bencode conventions.
func extractFieldName(f reflect.StructField) string {
	name, _ := parseTag(f.Tag.Get("bencode"))
	if name == "" {
		if f.Anonymous {
			// Anonymous fields without tags have already been processed
			return ""
		}
		name = f.Name
	}
	return name
}
