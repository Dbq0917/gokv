package util

import (
	"errors"
	"reflect"
)

// CheckKeyAndValue returns an error if k == "" or if v == nil
func CheckKeyAndValue(k string, v any) error {
	if err := CheckKey(k); err != nil {
		return err
	}
	return CheckVal(v)
}

// CheckKey returns an error if k == ""
func CheckKey(k string) error {
	if k == "" {
		return errors.New("the passed key is an empty string, which is invalid")
	}
	return nil
}

// CheckVal returns an error if v == nil
func CheckVal(v any) error {
	if v == nil {
		return errors.New("the passed value is nil, which is not allowed")
	}
	return nil
}

// CheckPointer returns an error if v is not a non-nil pointer.
func CheckPointer(v any) error {
	if err := CheckVal(v); err != nil {
		return err
	}
	value := reflect.ValueOf(v)
	if value.Kind() != reflect.Ptr {
		return errors.New("the passed value is not a pointer")
	}
	if value.IsNil() {
		return errors.New("the passed pointer is nil")
	}
	return nil
}

// ResetPointer sets the value pointed to by v to its type's zero value.
// It is used before unmarshalling into a pointer that may still contain data
// from an earlier read or update attempt.
func ResetPointer(v any) error {
	if err := CheckPointer(v); err != nil {
		return err
	}
	value := reflect.ValueOf(v)
	value.Elem().Set(reflect.Zero(value.Elem().Type()))
	return nil
}
