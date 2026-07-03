// A minimal, deterministic msgpack encoder for Hyperliquid action hashing.
// Hyperliquid computes the action hash over msgpack(action); the encoding must
// be byte-exact and key order must be stable. We therefore use OrderedMap for
// objects (preserving insertion order) and implement only the msgpack types HL
// actions use.
package signing

import (
	"encoding/json"
	"errors"
	"math"
	"sort"
)

// jsonMarshal is a thin wrapper so MarshalJSON can recurse through values
// (including nested OrderedMaps, which implement json.Marshaler themselves).
func jsonMarshal(v any) ([]byte, error) { return json.Marshal(v) }

// OrderedMap is a string-keyed map with deterministic key order, used so the
// msgpack encoding of an action matches Hyperliquid's reference encoding exactly.
type OrderedMap struct {
	keys   []string
	values map[string]any
}

// NewOrderedMap creates an empty ordered map.
func NewOrderedMap() *OrderedMap {
	return &OrderedMap{values: make(map[string]any)}
}

// Set inserts or updates a key, preserving first-insertion order.
func (m *OrderedMap) Set(key string, val any) *OrderedMap {
	if _, ok := m.values[key]; !ok {
		m.keys = append(m.keys, key)
	}
	m.values[key] = val
	return m
}

// Len returns the number of entries.
func (m *OrderedMap) Len() int { return len(m.keys) }

// MarshalJSON renders the map preserving insertion order, so the JSON sent in
// the exchange envelope matches the field order used to compute the action hash.
func (m *OrderedMap) MarshalJSON() ([]byte, error) {
	var b []byte
	b = append(b, '{')
	for i, k := range m.keys {
		if i > 0 {
			b = append(b, ',')
		}
		kb, err := jsonMarshal(k)
		if err != nil {
			return nil, err
		}
		b = append(b, kb...)
		b = append(b, ':')
		vb, err := jsonMarshal(m.values[k])
		if err != nil {
			return nil, err
		}
		b = append(b, vb...)
	}
	b = append(b, '}')
	return b, nil
}

func encodeValue(b *[]byte, v any) error {
	switch x := v.(type) {
	case nil:
		*b = append(*b, 0xc0)
	case bool:
		if x {
			*b = append(*b, 0xc3)
		} else {
			*b = append(*b, 0xc2)
		}
	case string:
		encodeString(b, x)
	case int:
		encodeInt(b, int64(x))
	case int64:
		encodeInt(b, x)
	case uint64:
		encodeUint(b, x)
	case float64:
		encodeFloat(b, x)
	case *OrderedMap:
		return encodeOrderedMap(b, x)
	case map[string]any:
		// Fallback: sort keys for determinism (prefer OrderedMap for HL parity).
		return encodeSortedMap(b, x)
	case []any:
		return encodeArray(b, x)
	default:
		return errors.New("msgpack: unsupported type")
	}
	return nil
}

func encodeString(b *[]byte, s string) {
	n := len(s)
	switch {
	case n < 32:
		*b = append(*b, 0xa0|byte(n))
	case n < 256:
		*b = append(*b, 0xd9, byte(n))
	case n < 65536:
		*b = append(*b, 0xda, byte(n>>8), byte(n))
	default:
		*b = append(*b, 0xdb, byte(n>>24), byte(n>>16), byte(n>>8), byte(n))
	}
	*b = append(*b, s...)
}

func encodeInt(b *[]byte, n int64) {
	if n >= 0 {
		encodeUint(b, uint64(n))
		return
	}
	switch {
	case n >= -32:
		*b = append(*b, byte(n))
	case n >= -128:
		*b = append(*b, 0xd0, byte(n))
	case n >= -32768:
		*b = append(*b, 0xd1, byte(n>>8), byte(n))
	case n >= -2147483648:
		*b = append(*b, 0xd2, byte(n>>24), byte(n>>16), byte(n>>8), byte(n))
	default:
		*b = append(*b, 0xd3)
		for i := 7; i >= 0; i-- {
			*b = append(*b, byte(n>>(8*i)))
		}
	}
}

func encodeUint(b *[]byte, n uint64) {
	switch {
	case n < 128:
		*b = append(*b, byte(n))
	case n < 256:
		*b = append(*b, 0xcc, byte(n))
	case n < 65536:
		*b = append(*b, 0xcd, byte(n>>8), byte(n))
	case n < 4294967296:
		*b = append(*b, 0xce, byte(n>>24), byte(n>>16), byte(n>>8), byte(n))
	default:
		*b = append(*b, 0xcf)
		for i := 7; i >= 0; i-- {
			*b = append(*b, byte(n>>(8*i)))
		}
	}
}

func encodeFloat(b *[]byte, f float64) {
	bits := math.Float64bits(f)
	*b = append(*b, 0xcb)
	for i := 7; i >= 0; i-- {
		*b = append(*b, byte(bits>>(8*i)))
	}
}

func encodeMapHeader(b *[]byte, n int) {
	switch {
	case n < 16:
		*b = append(*b, 0x80|byte(n))
	case n < 65536:
		*b = append(*b, 0xde, byte(n>>8), byte(n))
	default:
		*b = append(*b, 0xdf, byte(n>>24), byte(n>>16), byte(n>>8), byte(n))
	}
}

func encodeArrayHeader(b *[]byte, n int) {
	switch {
	case n < 16:
		*b = append(*b, 0x90|byte(n))
	case n < 65536:
		*b = append(*b, 0xdc, byte(n>>8), byte(n))
	default:
		*b = append(*b, 0xdd, byte(n>>24), byte(n>>16), byte(n>>8), byte(n))
	}
}

func encodeOrderedMap(b *[]byte, m *OrderedMap) error {
	encodeMapHeader(b, len(m.keys))
	for _, k := range m.keys {
		encodeString(b, k)
		if err := encodeValue(b, m.values[k]); err != nil {
			return err
		}
	}
	return nil
}

func encodeSortedMap(b *[]byte, m map[string]any) error {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	encodeMapHeader(b, len(keys))
	for _, k := range keys {
		encodeString(b, k)
		if err := encodeValue(b, m[k]); err != nil {
			return err
		}
	}
	return nil
}

func encodeArray(b *[]byte, a []any) error {
	encodeArrayHeader(b, len(a))
	for _, v := range a {
		if err := encodeValue(b, v); err != nil {
			return err
		}
	}
	return nil
}
