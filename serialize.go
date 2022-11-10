package main

type Serializable interface {
	Serialize() Serialized
	Deserialize(Serialized) interface{} // MAYBE: use links everywhere, and not return
}

type Serialized interface {
	serialized()
}

type SerializedBool bool
func (SerializedBool) serialized() {}

type SerializedInt int
func (SerializedInt) serialized() {}

type SerializedString string
func (SerializedString) serialized() {}

type SerializedList []Serialized
func (SerializedList) serialized() {}

type SerializedMap map[string]Serialized
func (SerializedMap) serialized() {}

func decodeToSerialized(v interface{}) Serialized {
	if b, ok := v.(bool); ok { return SerializedBool(b) }
	if i, ok := v.(int); ok { return SerializedInt(i) }
	if s, ok := v.(string); ok { return SerializedString(s) }
	if l, ok := v.([]interface{}); ok {
		list := make([]Serialized, 0, len(l))
		for _, value := range l { list = append(list, decodeToSerialized(value)) }
		return SerializedList(list)
	}
	if m, ok := v.(map[string]interface{}); ok {
		_map := make(map[string]Serialized)
		for key, value := range m { _map[key] = decodeToSerialized(value) }
		return SerializedMap(_map)
	}
	panic("cannot decode to serialized")
}
