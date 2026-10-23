package fast

import "encoding/json"

// OpCounts tallies executed instructions by coarse class for `mlvm run` statistics. It is
// execution metadata, not part of the state witness. It is a struct rather than a map
// because the interpreter bumps it on every instruction, and string-keyed map updates were
// a measurable share of each step (more so in the WebAssembly build). It serializes
// exactly like the map[string]uint64 it replaced: only non-zero classes, keys sorted.
type OpCounts struct {
	Arith   uint64
	Float   uint64
	FloatLS uint64
	Mem     uint64
	// Other keeps any other keys a snapshot carried, so it round-trips unchanged.
	Other map[string]uint64
}

// Get returns a count by its JSON key ("arith", "float", "floatLS", "mem").
func (c OpCounts) Get(key string) uint64 {
	switch key {
	case "arith":
		return c.Arith
	case "float":
		return c.Float
	case "floatLS":
		return c.FloatLS
	case "mem":
		return c.Mem
	}
	return c.Other[key]
}

func (c OpCounts) MarshalJSON() ([]byte, error) {
	m := map[string]uint64{}
	for k, v := range c.Other {
		m[k] = v
	}
	for _, kv := range []struct {
		key string
		n   uint64
	}{{"arith", c.Arith}, {"float", c.Float}, {"floatLS", c.FloatLS}, {"mem", c.Mem}} {
		if kv.n != 0 {
			m[kv.key] = kv.n
		}
	}
	return json.Marshal(m)
}

func (c *OpCounts) UnmarshalJSON(data []byte) error {
	var m map[string]uint64
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}
	*c = OpCounts{}
	for k, v := range m {
		switch k {
		case "arith":
			c.Arith = v
		case "float":
			c.Float = v
		case "floatLS":
			c.FloatLS = v
		case "mem":
			c.Mem = v
		default:
			if c.Other == nil {
				c.Other = map[string]uint64{}
			}
			c.Other[k] = v
		}
	}
	return nil
}
