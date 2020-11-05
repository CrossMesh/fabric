package common

import (
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"unsafe"
)

// ParamError raised when invalid param detected.
type ParamError struct {
	Msg string
}

func (e *ParamError) Error() string {
	return e.Msg
}

// ParamValidateFunction validates raw arg, converts to actual value and save to destination.
type ParamValidateFunction func(a *ParamArgument, raw string) (bool, error)

// ParamArgumentWriteDestinationUint writes new uint value to destination.
func ParamArgumentWriteDestinationUint(a *ParamArgument, v uint) (bool, error) {
	changed := false
	if a.Destination != nil {
		switch r := a.Destination.(type) {
		case uint:
			if r != v {
				a.Destination = v
				changed = true
			}
		case *uint:
			if *r != v {
				*r = v
				changed = true
			}
		default:
			return false, errors.New("destination is not of uint or *uint type")
		}
	}
	return changed, nil
}

// UintParamValidateFunction is default validation function for uint type.
func uintParamValidateFunction(a *ParamArgument, raw string, validate func(uint) error) (bool, error) {
	ui, err := strconv.ParseUint(raw, 10, 8*int(unsafe.Sizeof(uint(0))))
	if err != nil {
		return false, err
	}
	v := uint(ui)
	if validate != nil {
		if err = validate(v); err != nil {
			return false, err
		}
	}
	return ParamArgumentWriteDestinationUint(a, v)
}

// UintParamValidateFunction is default validation function for uint type.
func UintParamValidateFunction(a *ParamArgument, raw string) (bool, error) {
	return uintParamValidateFunction(a, raw, nil)
}

// UintParamValidation builds UintParamValidateFunction.
func UintParamValidation(validate func(x uint) error) ParamValidateFunction {
	return func(a *ParamArgument, raw string) (bool, error) {
		return uintParamValidateFunction(a, raw, validate)
	}
}

// ParamArgumentWriteDestinationUint32 writes new uint32 value to destination.
func ParamArgumentWriteDestinationUint32(a *ParamArgument, v uint32) (bool, error) {
	changed := false
	if a.Destination != nil {
		switch r := a.Destination.(type) {
		case uint32:
			if r != v {
				a.Destination = v
				changed = true
			}
		case *uint32:
			if *r != v {
				*r = v
				changed = true
			}
		default:
			return false, errors.New("destination is not of uint32 or *uint32 type")
		}
	}
	return changed, nil
}

func uint32ParamValidateFunction(a *ParamArgument, raw string, validate func(x uint32) error) (bool, error) {
	ui, err := strconv.ParseUint(raw, 10, 32)
	if err != nil {
		return false, err
	}
	v := uint32(ui)
	if validate != nil {
		if err = validate(v); err != nil {
			return false, err
		}
	}
	return ParamArgumentWriteDestinationUint32(a, uint32(ui))
}

// Uint32ParamValidateFunction is default validation function for uint32 type.
func Uint32ParamValidateFunction(a *ParamArgument, raw string) (bool, error) {
	return uintParamValidateFunction(a, raw, nil)
}

// Uint32ParamValidation builds Uint32ParamValidateFunction.
func Uint32ParamValidation(validate func(x uint32) error) ParamValidateFunction {
	return func(a *ParamArgument, raw string) (bool, error) {
		return uint32ParamValidateFunction(a, raw, validate)
	}
}

// ParamArgumentWriteDestinationInt writes new int value to destination.
func ParamArgumentWriteDestinationInt(a *ParamArgument, v int) (bool, error) {
	changed := false
	if a.Destination != nil {
		switch r := a.Destination.(type) {
		case int:
			if r != v {
				a.Destination = v
				changed = true
			}
		case *int:
			if *r != v {
				*r = v
				changed = true
			}
		default:
			return false, errors.New("destination is not of int or *int type")
		}
	}
	return changed, nil
}

func intParamValidateFunction(a *ParamArgument, raw string, validate func(x int) error) (bool, error) {
	ui, err := strconv.ParseInt(raw, 10, 8*int(unsafe.Sizeof(int(0))))
	if err != nil {
		return false, err
	}
	return ParamArgumentWriteDestinationInt(a, int(ui))
}

// IntParamValidateFunction is default validation function for int type.
func IntParamValidateFunction(a *ParamArgument, raw string) (bool, error) {
	return intParamValidateFunction(a, raw, nil)
}

// IntParamValidation builds IntParamValidateFunction.
func IntParamValidation(validate func(x int) error) ParamValidateFunction {
	return func(a *ParamArgument, raw string) (bool, error) {
		return intParamValidateFunction(a, raw, validate)
	}
}

// ParamArgumentWriteDestinationString writes new string value to destination.
func ParamArgumentWriteDestinationString(a *ParamArgument, v string) (bool, error) {
	changed := false
	if a.Destination != nil {
		switch r := a.Destination.(type) {
		case string:
			if r != v {
				a.Destination = v
				changed = true
			}
		case *string:
			if *r != v {
				*r = v
				changed = true
			}
		default:
			return false, errors.New("destination is not of string or *string type")
		}
	}
	return changed, nil
}

// StringParamValidateFunction is default validation function for int type.
func stringParamValidateFunction(a *ParamArgument, raw string, validate func(s string) error) (bool, error) {
	if validate != nil {
		if err := validate(raw); err != nil {
			return false, err
		}
	}
	return ParamArgumentWriteDestinationString(a, raw)
}

// StringParamValidateFunction is default validation function for int type.
func StringParamValidateFunction(a *ParamArgument, raw string) (bool, error) {
	return stringParamValidateFunction(a, raw, nil)
}

// StringParamValidation builds StringParamValidateFunction.
func StringParamValidation(validate func(s string) error) ParamValidateFunction {
	return func(a *ParamArgument, raw string) (bool, error) {
		return stringParamValidateFunction(a, raw, validate)
	}
}

func boolParamValidateFunction(a *ParamArgument, raw string, validate func(x bool) error) (bool, error) {
	b, err := strconv.ParseBool(raw)
	if err != nil {
		return false, err
	}
	if validate != nil {
		if err = validate(b); err != nil {
			return false, err
		}
	}
	return ParamArgumentWriteDestinationBool(a, b)
}

// ParamArgumentWriteDestinationBool writes new bool value to destination.
func ParamArgumentWriteDestinationBool(a *ParamArgument, b bool) (bool, error) {
	changed := false
	if a.Destination != nil {
		switch r := a.Destination.(type) {
		case bool:
			if r != b {
				a.Destination = b
				changed = true
			}
		case *bool:
			if *r != b {
				*r = b
				changed = true
			}
		default:
			return false, errors.New("destination is not of string or *string type")
		}
	}
	return changed, nil
}

// BoolParamValidation builds BoolParamValidateFunction.
func BoolParamValidation(validate func(b bool) error) ParamValidateFunction {
	return func(a *ParamArgument, raw string) (bool, error) {
		return boolParamValidateFunction(a, raw, validate)
	}
}

type ParamArgument struct {
	Name        string
	Validator   ParamValidateFunction
	Destination interface{}
	BeforeParse func(index int)
	Description string

	validate ParamValidateFunction
}

type ParamItem struct {
	Name        string
	Description string
	Args        []*ParamArgument
}

type ParamList struct {
	Item []*ParamItem

	nameMap map[string]*ParamItem
}

// Compile prepares fields for parsing.
func (l *ParamList) Compile() error {
	l.nameMap = map[string]*ParamItem{}
	for _, item := range l.Item {
		if item == nil {
			continue
		}
		old, _ := l.nameMap[item.Name]
		if old != nil {
			return fmt.Errorf("item \"%v\" duplicated", item.Name)
		}
		l.nameMap[item.Name] = item

		// determine validation function.
		for _, arg := range item.Args {
			if arg == nil {
			}
			if validate := arg.Validator; validate == nil {
				// default validator.
				if arg.Destination == nil { // accept anything?
					continue
				}
				switch t := arg.Destination.(type) {
				case uint, *uint:
					arg.validate = UintParamValidateFunction
				case uint32, *uint32:
					arg.validate = Uint32ParamValidateFunction
				case int, *int:
					arg.validate = IntParamValidateFunction
				case string, *string:
					arg.validate = StringParamValidateFunction
				default:
					return fmt.Errorf("unsupported default validation for type \"%v\"", reflect.TypeOf(t).Name())
				}

			} else {
				arg.validate = arg.Validator
			}
		}
	}
	return nil
}

// ParseArgs parses input arguments.
func (l *ParamList) ParseArgs(args []string) (parsed uint, changes uint, err error) {
	nameMap := l.nameMap
	if nameMap == nil {
		if err = l.Compile(); err != nil {
			return 0, 0, err
		}
		nameMap = l.nameMap
	}

	for i := 0; i < len(args); {
		name := args[i]
		item, _ := l.nameMap[name]
		if item == nil {
			return parsed, changes, &ParamError{
				Msg: "unknown param \"" + name + "\"",
			}
		}
		ai := 1
		for argIdx := 0; argIdx < len(item.Args); argIdx++ {
			argItem := item.Args[argIdx]
			if argItem == nil {
				continue
			}
			if ai >= len(args) {
				return parsed, changes, &ParamError{
					Msg: fmt.Sprintf("missing %v for param \"%v\"", argItem.Name, name),
				}
			}
			raw := args[i+ai]
			ai++
			if argItem.validate == nil {
				break
			}
			if changed, err := argItem.validate(argItem, raw); err != nil {
				return parsed, changes, err
			} else if changed {
				changes++
			}
		}
		i += ai
		parsed++
	}

	return parsed, changes, nil
}
