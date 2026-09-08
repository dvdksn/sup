package sup

import (
	"context"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	lua "github.com/yuin/gopher-lua"
)

type ConfigContext struct {
	Repo, Name string
	Args       map[string]string
}

type loadedConfig struct {
	L            *lua.LState
	table        *lua.LTable
	cancel       context.CancelFunc
	declarations map[string]ConfigArg
}

func (c *loadedConfig) Close() { c.cancel(); c.L.Close() }

func configure(path string, ctx ConfigContext, extra []string) (*Environment, error) {
	c, err := loadConfig(path)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	ctx.Args, err = c.arguments(ctx.Args)
	if err != nil {
		return nil, err
	}
	return c.environment(ctx, extra)
}

func loadConfig(path string) (_ *loadedConfig, err error) {
	L := lua.NewState()
	timeout, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer func() {
		if err != nil {
			cancel()
			L.Close()
		}
	}()
	L.SetContext(timeout)
	var config *lua.LTable
	L.PreloadModule("sup", func(L *lua.LState) int {
		mod := L.NewTable()
		L.SetField(mod, "setup", L.NewFunction(func(L *lua.LState) int {
			if config != nil {
				L.RaiseError("sup.setup may only be called once")
				return 0
			}
			config = L.CheckTable(1)
			return 0
		}))
		L.Push(mod)
		return 1
	})
	// Keep return-table configs working during the port.
	chunk, err := L.LoadFile(path)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	if err = L.CallByParam(lua.P{Fn: chunk, NRet: 1, Protect: true}); err != nil {
		return nil, err
	}
	returned := L.Get(-1)
	L.Pop(1)
	if returned != lua.LNil {
		if config != nil {
			return nil, errors.New("use sup.setup or a returned config table, not both")
		}
		config, _ = returned.(*lua.LTable)
	}
	if config == nil {
		return nil, errors.New("config must call sup.setup({...})")
	}
	var fieldErr error
	config.ForEach(func(k, v lua.LValue) {
		if k.String() != "kits" && k.String() != "configure" && k.String() != "defaults" && k.String() != "repos" && k.String() != "args" && k.String() != "name" {
			fieldErr = fmt.Errorf("config.%s: unknown field", k.String())
		}
	})
	if fieldErr != nil {
		return nil, fieldErr
	}
	c := &loadedConfig{L: L, table: config, cancel: cancel, declarations: map[string]ConfigArg{}}
	if v := config.RawGetString("args"); v != lua.LNil {
		if err = bind(v, reflect.ValueOf(&c.declarations).Elem(), "args", map[*lua.LTable]bool{}); err != nil {
			return nil, err
		}
	}
	if err = c.validateDeclarations(); err != nil {
		return nil, err
	}
	if v := config.RawGetString("name"); v != lua.LNil {
		if _, ok := v.(*lua.LFunction); !ok {
			return nil, errors.New("config.name must be a function")
		}
	}
	return c, nil
}

func (c *loadedConfig) environment(ctx ConfigContext, extra []string) (*Environment, error) {
	L, config := c.L, c.table
	var err error
	aliases := map[string]string{}
	if v := config.RawGetString("kits"); v != lua.LNil {
		if err = bind(v, reflect.ValueOf(&aliases).Elem(), "kits", map[*lua.LTable]bool{}); err != nil {
			return nil, err
		}
	}
	envTable, err := compose(L, config, ctx)
	if err != nil {
		return nil, err
	}
	for _, k := range []string{"workspace", "additionalWorkspaces"} {
		if envTable.RawGetString(k) != lua.LNil {
			return nil, errors.New("sup environments cannot declare workspace mounts")
		}
	}
	// Resolve aliases before decoding into the native schema.
	kits := L.NewTable()
	seen := map[string]Kit{}
	add := func(v lua.LValue) error {
		var kit Kit
		switch v := v.(type) {
		case lua.LString:
			kit.Source = string(v)
		case *lua.LTable:
			if err := bind(v, reflect.ValueOf(&kit).Elem(), "kits", map[*lua.LTable]bool{}); err != nil {
				return err
			}
		default:
			return errors.New("kits: expected an alias/source string or kit table")
		}
		if alias, ok := aliases[kit.Source]; ok {
			kit.Source = alias
		}
		if strings.HasPrefix(kit.Source, ".") {
			return fmt.Errorf("use an absolute path for local kits: %s", kit.Source)
		}
		if !strings.Contains(kit.Source, "/") {
			return fmt.Errorf("unknown kit alias or invalid source: %s", kit.Source)
		}
		if old, exists := seen[kit.Source]; exists {
			if !reflect.DeepEqual(old.Args, kit.Args) && !(len(old.Args) == 0 && len(kit.Args) == 0) {
				return fmt.Errorf("conflicting arguments for kit: %s", kit.Source)
			}
			return nil
		}
		seen[kit.Source] = kit
		entry := L.NewTable()
		entry.RawSetString("source", lua.LString(kit.Source))
		if len(kit.Args) > 0 {
			args := L.NewTable()
			for k, v := range kit.Args {
				args.RawSetString(k, lua.LString(v))
			}
			entry.RawSetString("args", args)
		}
		kits.Append(entry)
		return nil
	}
	if v := envTable.RawGetString("kits"); v != lua.LNil {
		list, ok := v.(*lua.LTable)
		if !ok {
			return nil, errors.New("kits: expected list")
		}
		if err := array(list, "kits"); err != nil {
			return nil, err
		}
		for i := 1; i <= list.Len(); i++ {
			if err := add(list.RawGetInt(i)); err != nil {
				return nil, err
			}
		}
	}
	for _, s := range extra {
		if err := add(lua.LString(s)); err != nil {
			return nil, err
		}
	}
	envTable.RawSetString("kits", kits)
	var env Environment
	if err = bind(envTable, reflect.ValueOf(&env).Elem(), "environment", map[*lua.LTable]bool{}); err != nil {
		return nil, err
	}
	if env.Name != "" && env.Name != ctx.Name {
		return nil, errors.New("environment must use ctx.name as the environment name")
	}
	env.Name = ctx.Name
	if env.SchemaVersion == "" {
		env.SchemaVersion = "1"
	}
	if err = validate(&env); err != nil {
		return nil, err
	}
	return &env, nil
}
func validate(env *Environment) error {
	if env.SchemaVersion != "1" {
		return errors.New("schemaVersion must be the string \"1\"")
	}
	if strings.TrimSpace(env.Agent) == "" {
		return errors.New("environment must supply an agent")
	}
	if env.SandboxOptions != nil && env.SandboxOptions.CPUs < 0 {
		return errors.New("sandboxOptions.cpus must not be negative")
	}
	if !namePattern.MatchString(env.Name) || env.Name == "default" {
		return errors.New("invalid saved environment name")
	}
	return nil
}

func array(t *lua.LTable, path string) error {
	count := 0
	var err error
	t.ForEach(func(k, v lua.LValue) {
		n, ok := k.(lua.LNumber)
		if !ok || float64(n) != math.Trunc(float64(n)) || n < 1 || int(n) > t.Len() {
			err = fmt.Errorf("%s: expected a contiguous list", path)
		}
		count++
	})
	if count != t.Len() {
		err = fmt.Errorf("%s: expected a contiguous list", path)
	}
	return err
}

// bind validates Lua values against the native schema while preserving empty lists/maps.
// It deliberately performs no string/number coercion, and reports the field path.
func bind(v lua.LValue, out reflect.Value, path string, active map[*lua.LTable]bool) error {
	bad := func(want string) error { return fmt.Errorf("%s: expected %s, got %s", path, want, v.Type()) }
	if out.Kind() == reflect.Pointer {
		out.Set(reflect.New(out.Type().Elem()))
		return bind(v, out.Elem(), path, active)
	}
	switch out.Kind() {
	case reflect.String:
		s, ok := v.(lua.LString)
		if !ok {
			return bad("string")
		}
		out.SetString(string(s))
		return nil
	case reflect.Bool:
		b, ok := v.(lua.LBool)
		if !ok {
			return bad("boolean")
		}
		out.SetBool(bool(b))
		return nil
	case reflect.Int:
		n, ok := v.(lua.LNumber)
		if !ok || math.IsNaN(float64(n)) || math.IsInf(float64(n), 0) || float64(n) != math.Trunc(float64(n)) || math.Abs(float64(n)) > 9007199254740991 {
			return bad("integer")
		}
		out.SetInt(int64(n))
		return nil
	}
	t, ok := v.(*lua.LTable)
	if !ok {
		return bad("table")
	}
	if active[t] {
		return fmt.Errorf("%s: cyclic table", path)
	}
	active[t] = true
	defer delete(active, t)
	switch out.Kind() {
	case reflect.Slice:
		if err := array(t, path); err != nil {
			return err
		}
		out.Set(reflect.MakeSlice(out.Type(), t.Len(), t.Len()))
		for i := 0; i < t.Len(); i++ {
			if err := bind(t.RawGetInt(i+1), out.Index(i), fmt.Sprintf("%s[%d]", path, i+1), active); err != nil {
				return err
			}
		}
	case reflect.Map:
		out.Set(reflect.MakeMap(out.Type()))
		var err error
		t.ForEach(func(k, v lua.LValue) {
			if err != nil {
				return
			}
			s, ok := k.(lua.LString)
			if !ok {
				err = fmt.Errorf("%s: expected string keys", path)
				return
			}
			dest := reflect.New(out.Type().Elem()).Elem()
			err = bind(v, dest, path+"."+string(s), active)
			if err == nil {
				out.SetMapIndex(reflect.ValueOf(string(s)), dest)
			}
		})
		return err
	case reflect.Struct:
		fields := map[string]int{}
		for i := 0; i < out.NumField(); i++ {
			fields[strings.Split(out.Type().Field(i).Tag.Get("json"), ",")[0]] = i
		}
		var err error
		t.ForEach(func(k, v lua.LValue) {
			if err != nil {
				return
			}
			s, ok := k.(lua.LString)
			if !ok {
				err = fmt.Errorf("%s: expected named fields", path)
				return
			}
			i, ok := fields[string(s)]
			if !ok {
				err = fmt.Errorf("%s.%s: unknown field", path, s)
				return
			}
			err = bind(v, out.Field(i), path+"."+string(s), active)
		})
		return err
	default:
		return fmt.Errorf("%s: unsupported schema type %s", path, out.Kind())
	}
	return nil
}

func defaultConfig(home string) string {
	return filepath.Join(xdg("XDG_CONFIG_HOME", filepath.Join(home, ".config")), "sup", "config.lua")
}
