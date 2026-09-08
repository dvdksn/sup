package sup

import (
	"errors"
	"fmt"
	"reflect"
	"strings"

	lua "github.com/yuin/gopher-lua"
)

func compose(L *lua.LState, config *lua.LTable, ctx ConfigContext) (*lua.LTable, error) {
	defaults := config.RawGetString("defaults")
	legacy := config.RawGetString("configure")
	if legacy != lua.LNil {
		if defaults != lua.LNil || config.RawGetString("repos") != lua.LNil {
			return nil, errors.New("configure cannot be combined with defaults or repos; migrate to defaults")
		}
		if _, ok := legacy.(*lua.LFunction); !ok {
			return nil, errors.New("config.configure must be a function")
		}
		defaults = legacy
	}
	if fn, ok := defaults.(*lua.LFunction); ok {
		input := luaContext(L, ctx)
		if err := L.CallByParam(lua.P{Fn: fn, NRet: 1, Protect: true}, input); err != nil {
			return nil, err
		}
		defaults = L.Get(-1)
		L.Pop(1)
	}
	base, ok := defaults.(*lua.LTable)
	if !ok {
		return nil, errors.New("defaults must be an environment table or a function returning an environment table")
	}
	var overlay *lua.LTable
	if repos := config.RawGetString("repos"); repos != lua.LNil {
		entries, ok := repos.(*lua.LTable)
		if !ok {
			return nil, errors.New("repos must be a table keyed by OWNER/REPO")
		}
		var err error
		entries.ForEach(func(k, v lua.LValue) {
			if err != nil {
				return
			}
			name, ok := k.(lua.LString)
			if !ok || !repoPattern.MatchString(string(name)) {
				err = errors.New("repos keys must be OWNER/REPO")
				return
			}
			t, ok := v.(*lua.LTable)
			if !ok {
				err = fmt.Errorf("repos[%q]: expected an environment table", name)
				return
			}
			if string(name) == ctx.Repo {
				overlay = t
			}
		})
		if err != nil {
			return nil, err
		}
	}
	// Copy so composition never mutates a table shared by config code.
	copied, err := copyLua(L, base, map[*lua.LTable]bool{})
	if err != nil {
		return nil, err
	}
	result := copied.(*lua.LTable)
	if overlay != nil {
		patch, err := copyLua(L, overlay, map[*lua.LTable]bool{})
		if err != nil {
			return nil, err
		}
		if err = merge(L, result, patch.(*lua.LTable), reflect.TypeOf(Environment{}), "environment"); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func copyLua(L *lua.LState, v lua.LValue, active map[*lua.LTable]bool) (lua.LValue, error) {
	t, ok := v.(*lua.LTable)
	if !ok {
		return v, nil
	}
	if active[t] {
		return nil, errors.New("environment contains a cyclic table")
	}
	active[t] = true
	defer delete(active, t)
	result := L.NewTable()
	var err error
	t.ForEach(func(k, v lua.LValue) {
		if err != nil {
			return
		}
		var copied lua.LValue
		copied, err = copyLua(L, v, active)
		if err == nil {
			result.RawSet(k, copied)
		}
	})
	return result, err
}

// Mapping fields merge recursively. Kits append; all other lists replace.
// Schema types distinguish empty lists from empty maps without Lua markers.
func merge(L *lua.LState, base, patch *lua.LTable, typ reflect.Type, path string) error {
	var err error
	patch.ForEach(func(k, v lua.LValue) {
		if err != nil {
			return
		}
		old := base.RawGet(k)
		a, aok := old.(*lua.LTable)
		b, bok := v.(*lua.LTable)
		field := fieldType(typ, k.String())
		for field != nil && field.Kind() == reflect.Pointer {
			field = field.Elem()
		}
		if aok && bok && path == "environment" && k.String() == "kits" {
			if err = array(a, "defaults.kits"); err != nil {
				return
			}
			if err = array(b, "repos.kits"); err != nil {
				return
			}
			for i := 1; i <= b.Len(); i++ {
				a.Append(b.RawGetInt(i))
			}
		} else if aok && bok && field != nil && (field.Kind() == reflect.Struct || field.Kind() == reflect.Map) {
			err = merge(L, a, b, field, path+"."+k.String())
		} else {
			base.RawSet(k, v)
		}
	})
	return err
}
func fieldType(typ reflect.Type, name string) reflect.Type {
	if typ.Kind() == reflect.Map {
		return typ.Elem()
	}
	if typ.Kind() == reflect.Struct {
		for i := 0; i < typ.NumField(); i++ {
			f := typ.Field(i)
			if strings.Split(f.Tag.Get("json"), ",")[0] == name {
				return f.Type
			}
		}
	}
	return nil
}
