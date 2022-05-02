package main

import (
	"github.com/0leksandr/my.go"
	"reflect"
	"testing"
)

func TestInterfaces(t *testing.T) {
	types := my.Types(false)
	parsed := my.ParseTypes()
	interfaces := make([]reflect.Type, 0)
	structs := make([]reflect.Type, 0)
	for _, _type := range types {
		switch _type.Kind() {
			case reflect.Interface: interfaces = append(interfaces, _type)
			case reflect.Struct:    structs = append(structs, _type)
		}
	}
	for _, _interface := range interfaces {
		for _, _struct := range structs {
			if _struct.Implements(_interface) || reflect.PtrTo(_struct).Implements(_interface) {
				my.Assert(
					t,
					parsed.Structs()[_struct.Name()].Implements(parsed.Interfaces()[_interface.Name()]),
					_struct.Name(),
					_interface.Name(),
				)
			}
		}
	}
}
