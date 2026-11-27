package evaluation

import (
	"bytes"
	"encoding/json"
	"math"
	"math/big"
	"reflect"
	"strconv"
	"strings"
)

func decodeNumbers(data []byte) (any, error) {
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	var value any
	if err := d.Decode(&value); err != nil {
		return nil, err
	}
	return value, nil
}

func isFloatLiteral(number json.Number) bool {
	return strings.ContainsAny(string(number), ".eE")
}

func numbersEqual(a, b json.Number, sameKind bool) bool {
	floatA, floatB := isFloatLiteral(a), isFloatLiteral(b)
	if sameKind && floatA != floatB {
		return false
	}
	if !floatA && !floatB {
		x, okA := new(big.Int).SetString(string(a), 10)
		y, okB := new(big.Int).SetString(string(b), 10)
		return okA && okB && x.Cmp(y) == 0
	}
	if floatA && floatB {
		fa, errA := strconv.ParseFloat(string(a), 64)
		fb, errB := strconv.ParseFloat(string(b), 64)
		return errA == nil && errB == nil && fa == fb
	}
	return mixedEqual(a, b, floatA)
}

func mixedEqual(a, b json.Number, aIsFloat bool) bool {
	floatText, intText := a, b
	if !aIsFloat {
		floatText, intText = b, a
	}
	f, err := strconv.ParseFloat(string(floatText), 64)
	n, ok := new(big.Int).SetString(string(intText), 10)
	if err != nil || !ok || math.IsInf(f, 0) {
		return false
	}
	exact, _ := new(big.Float).SetFloat64(f).Int(nil)
	return new(big.Float).SetFloat64(f).IsInt() && exact.Cmp(n) == 0
}

func jsonEqual(a, b any, sameKind bool) bool {
	switch x := a.(type) {
	case map[string]any:
		y, ok := b.(map[string]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for key, value := range x {
			other, present := y[key]
			if !present || !jsonEqual(value, other, sameKind) {
				return false
			}
		}
		return true
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if !jsonEqual(x[i], y[i], sameKind) {
				return false
			}
		}
		return true
	case json.Number:
		y, ok := b.(json.Number)
		return ok && numbersEqual(x, y, sameKind)
	}
	return reflect.DeepEqual(a, b)
}
