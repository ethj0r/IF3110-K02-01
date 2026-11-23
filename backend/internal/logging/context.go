package Logging

import (
	Context "context"
	Slog "log/slog"
	Sync "sync"
)

type scopeKey struct{}

// Scope holds correlation attributes for one unit of work (an HTTP request or a
// job). It is shared by pointer so that inner layers, such as authentication
// or access checks, can add attributes that the outer request log also sees.
type Scope struct {
	Mutex  Sync.Mutex
	Keys   []string
	Values map[string]Slog.Value
}

func newScope(Parent *Scope) *Scope {
	Created := &Scope{Values: map[string]Slog.Value{}}
	if Parent != nil {
		Parent.Mutex.Lock()
		Created.Keys = append(Created.Keys, Parent.Keys...)
		for Key, Value := range Parent.Values {
			Created.Values[Key] = Value
		}
		Parent.Mutex.Unlock()
	}
	return Created
}

func (Current *Scope) set(Key string, Value Slog.Value, Overwrite bool) {
	Current.Mutex.Lock()
	defer Current.Mutex.Unlock()
	if _, Exists := Current.Values[Key]; Exists {
		if Overwrite {
			Current.Values[Key] = Value
		}
		return
	}
	Current.Keys = append(Current.Keys, Key)
	Current.Values[Key] = Value
}

func scopeFrom(Ctx Context.Context) *Scope {
	if Ctx == nil {
		return nil
	}
	Current, _ := Ctx.Value(scopeKey{}).(*Scope)
	return Current
}

// NewScope starts a mutable attribute scope that inherits the parent's attributes.
func NewScope(Ctx Context.Context, Args ...any) Context.Context {
	Current := newScope(scopeFrom(Ctx))
	for _, Attr := range argsToAttrs(Args) {
		Current.set(Attr.Key, Attr.Value, true)
	}
	return Context.WithValue(Ctx, scopeKey{}, Current)
}

// With returns a child context whose logs carry the given key/value pairs in
// addition to every inherited attribute. The parent scope is not modified.
func With(Ctx Context.Context, Args ...any) Context.Context {
	return NewScope(Ctx, Args...)
}

// Annotate adds or replaces attributes on the nearest existing scope so that
// earlier layers (for example the request log) report them too. It is a no-op
// when the context has no scope.
func Annotate(Ctx Context.Context, Args ...any) {
	if Current := scopeFrom(Ctx); Current != nil {
		for _, Attr := range argsToAttrs(Args) {
			Current.set(Attr.Key, Attr.Value, true)
		}
	}
}

// AnnotateOnce adds attributes that are not already present, keeping the first value.
func AnnotateOnce(Ctx Context.Context, Args ...any) {
	if Current := scopeFrom(Ctx); Current != nil {
		for _, Attr := range argsToAttrs(Args) {
			Current.set(Attr.Key, Attr.Value, false)
		}
	}
}

// Attrs returns the attributes stored in the context scope in insertion order.
func Attrs(Ctx Context.Context) []Slog.Attr {
	Current := scopeFrom(Ctx)
	if Current == nil {
		return nil
	}
	Current.Mutex.Lock()
	defer Current.Mutex.Unlock()
	Result := make([]Slog.Attr, 0, len(Current.Keys))
	for _, Key := range Current.Keys {
		Result = append(Result, Slog.Attr{Key: Key, Value: Current.Values[Key]})
	}
	return Result
}

func argsToAttrs(Args []any) []Slog.Attr {
	var Result []Slog.Attr
	for Index := 0; Index < len(Args); Index++ {
		switch Value := Args[Index].(type) {
		case Slog.Attr:
			Result = append(Result, Value)
		case string:
			if Index+1 >= len(Args) {
				Result = append(Result, Slog.Any("!BADKEY", Value))
				continue
			}
			Result = append(Result, Slog.Any(Value, Args[Index+1]))
			Index++
		default:
			Result = append(Result, Slog.Any("!BADKEY", Value))
		}
	}
	return Result
}
