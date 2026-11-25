package evidence

import (
	"fmt"
	"reflect"
	"unicode/utf8"
)

func validateLineUTF8(value any) error {
	return validateLineValue(reflect.ValueOf(value), 0)
}

func validateLineValue(value reflect.Value, depth int) error {
	if !value.IsValid() {
		return nil
	}
	if depth > 100 {
		return fmt.Errorf("summary value exceeds nesting limit")
	}
	switch value.Kind() {
	case reflect.String:
		if !utf8.ValidString(value.String()) {
			return fmt.Errorf("summary value has invalid UTF-8")
		}
	case reflect.Interface, reflect.Pointer:
		if !value.IsNil() {
			return validateLineValue(value.Elem(), depth+1)
		}
	case reflect.Struct:
		for i := 0; i < value.NumField(); i++ {
			if err := validateLineValue(value.Field(i), depth+1); err != nil {
				return err
			}
		}
	case reflect.Slice, reflect.Array:
		if value.Type().Elem().Kind() == reflect.Uint8 {
			return nil
		}
		for i := 0; i < value.Len(); i++ {
			if err := validateLineValue(value.Index(i), depth+1); err != nil {
				return err
			}
		}
	case reflect.Map:
		it := value.MapRange()
		for it.Next() {
			if err := validateLineValue(it.Key(), depth+1); err != nil {
				return err
			}
			if err := validateLineValue(it.Value(), depth+1); err != nil {
				return err
			}
		}
	}
	return nil
}
