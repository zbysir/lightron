package lightron

import (
	"encoding/json"
	"fmt"
	"reflect"
)

// bindings are the Go functions the page can invoke (Options.Bind).
type bindings struct {
	fns map[string]reflect.Value
}

var (
	errorType  = reflect.TypeOf((*error)(nil)).Elem()
	windowType = reflect.TypeOf((*Window)(nil))
)

func newBindings(m map[string]any) (*bindings, error) {
	b := &bindings{fns: map[string]reflect.Value{}}
	for name, f := range m {
		v := reflect.ValueOf(f)
		if v.Kind() != reflect.Func {
			return nil, fmt.Errorf("lightron: Bind[%q] is a %s, not a function", name, v.Kind())
		}
		t := v.Type()
		switch t.NumOut() {
		case 0:
		case 1:
		case 2:
			if !t.Out(1).Implements(errorType) {
				return nil, fmt.Errorf("lightron: Bind[%q]: a second result must be an error", name)
			}
		default:
			return nil, fmt.Errorf("lightron: Bind[%q]: return a value, an error, or (value, error)", name)
		}
		b.fns[name] = v
	}
	return b, nil
}

// call runs the function name with the page's arguments (JSON), w as its *Window parameter if it has one.
func (b *bindings) call(name string, w *Window, args []json.RawMessage) (result any, err error) {
	f, ok := b.fns[name]
	if !ok {
		return nil, fmt.Errorf("no function %q is bound", name)
	}
	defer func() {
		if r := recover(); r != nil {
			result, err = nil, fmt.Errorf("%s panicked: %v", name, r)
		}
	}()
	t := f.Type()
	var in []reflect.Value
	first := 0
	if t.NumIn() > 0 && t.In(0) == windowType {
		in = append(in, reflect.ValueOf(w))
		first = 1
	}
	want := t.NumIn() - first
	if t.IsVariadic() {
		if len(args) < want-1 {
			return nil, fmt.Errorf("%s takes at least %d arguments, got %d", name, want-1, len(args))
		}
	} else if len(args) != want {
		return nil, fmt.Errorf("%s takes %d arguments, got %d", name, want, len(args))
	}
	for i, raw := range args {
		var pt reflect.Type
		if t.IsVariadic() && first+i >= t.NumIn()-1 {
			pt = t.In(t.NumIn() - 1).Elem()
		} else {
			pt = t.In(first + i)
		}
		p := reflect.New(pt)
		if err := json.Unmarshal(raw, p.Interface()); err != nil {
			return nil, fmt.Errorf("%s: argument %d: %v", name, i+1, err)
		}
		in = append(in, p.Elem())
	}
	out := f.Call(in)
	switch len(out) {
	case 0:
		return nil, nil
	case 1:
		if t.Out(0).Implements(errorType) {
			if e, _ := out[0].Interface().(error); e != nil {
				return nil, e
			}
			return nil, nil
		}
		return out[0].Interface(), nil
	default:
		if e, _ := out[1].Interface().(error); e != nil {
			return nil, e
		}
		return out[0].Interface(), nil
	}
}
