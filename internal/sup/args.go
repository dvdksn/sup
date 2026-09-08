package sup

import (
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"text/tabwriter"

	lua "github.com/yuin/gopher-lua"
)

type ConfigArg struct {
	Description string   `json:"description,omitempty"`
	Default     *string  `json:"default,omitempty"`
	Required    bool     `json:"required,omitempty"`
	Choices     []string `json:"choices,omitempty"`
	Pattern     string   `json:"pattern,omitempty"`
}

func (c *loadedConfig) validateDeclarations() error {
	for key, d := range c.declarations {
		if !argNamePattern.MatchString(key) {
			return fmt.Errorf("args.%s: invalid argument name", key)
		}
		if d.Required && d.Default != nil {
			return fmt.Errorf("args.%s: required and default cannot be combined", key)
		}
		if d.Pattern != "" {
			if _, err := regexp.Compile("^(?:" + d.Pattern + ")$"); err != nil {
				return fmt.Errorf("args.%s.pattern: %w", key, err)
			}
		}
		if d.Default != nil {
			if err := d.check(key, *d.Default); err != nil {
				return fmt.Errorf("invalid default: %w", err)
			}
		}
	}
	return nil
}
func (d ConfigArg) check(key, value string) error {
	if len(d.Choices) > 0 {
		found := false
		for _, choice := range d.Choices {
			if value == choice {
				found = true
			}
		}
		if !found {
			return fmt.Errorf("argument %s must be one of: %s", key, strings.Join(d.Choices, ", "))
		}
	}
	if d.Pattern != "" && !regexp.MustCompile("^(?:"+d.Pattern+")$").MatchString(value) {
		return fmt.Errorf("argument %s does not match pattern %q", key, d.Pattern)
	}
	return nil
}
func (c *loadedConfig) arguments(given map[string]string) (map[string]string, error) {
	result := map[string]string{}
	for key, value := range given {
		d, ok := c.declarations[key]
		if !ok {
			return nil, fmt.Errorf("unknown argument %q; use sup args to inspect the config", key)
		}
		if err := d.check(key, value); err != nil {
			return nil, err
		}
		result[key] = value
	}
	for key, d := range c.declarations {
		if _, ok := result[key]; ok {
			continue
		}
		if d.Default != nil {
			result[key] = *d.Default
		} else if d.Required {
			return nil, fmt.Errorf("missing required argument: %s", key)
		}
	}
	return result, nil
}
func luaContext(L *lua.LState, ctx ConfigContext) *lua.LTable {
	t := L.NewTable()
	for k, v := range map[string]string{"repo": ctx.Repo, "name": ctx.Name} {
		t.RawSetString(k, lua.LString(v))
	}
	args := L.NewTable()
	for k, v := range ctx.Args {
		args.RawSetString(k, lua.LString(v))
	}
	t.RawSetString("args", args)
	return t
}
func (c *loadedConfig) name(ctx ConfigContext) (string, error) {
	fn, ok := c.table.RawGetString("name").(*lua.LFunction)
	if !ok {
		return ctx.Name, nil
	}
	if err := c.L.CallByParam(lua.P{Fn: fn, NRet: 1, Protect: true}, luaContext(c.L, ctx)); err != nil {
		return "", err
	}
	value := c.L.Get(-1)
	c.L.Pop(1)
	name, ok := value.(lua.LString)
	if !ok || string(name) == "" {
		return "", fmt.Errorf("name(ctx) must return a nonempty string")
	}
	resolved := safeDerivedName(string(name))
	if !namePattern.MatchString(resolved) || resolved == "default" {
		return "", fmt.Errorf("invalid environment name: %s", resolved)
	}
	return resolved, nil
}
func describeArgs(c *loadedConfig, out io.Writer) (int, error) {
	if len(c.declarations) == 0 {
		_, err := fmt.Fprintln(out, "No arguments declared.")
		return 0, err
	}
	keys := make([]string, 0, len(c.declarations))
	for key := range c.declarations {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "ARG\tDEFAULT\tREQUIRED\tCHOICES / PATTERN\tDESCRIPTION")
	for _, key := range keys {
		d := c.declarations[key]
		def := "—"
		if d.Default != nil {
			def = fmt.Sprintf("%q", *d.Default)
		}
		constraint := strings.Join(d.Choices, ", ")
		if d.Pattern != "" {
			if constraint != "" {
				constraint += "; "
			}
			constraint += d.Pattern
		}
		fmt.Fprintf(w, "%s\t%s\t%t\t%s\t%s\n", key, def, d.Required, display(constraint), display(d.Description))
	}
	return 0, w.Flush()
}

// Supplied arguments must match the saved snapshot. Unspecified values reuse it.
func matchArgs(state snapshot, given map[string]string) error {
	if len(given) == 0 {
		return nil
	}
	if state.Version == 1 {
		return fmt.Errorf("saved environment predates argument tracking; reconnect without arguments or choose a new --name")
	}
	saved := state.Args
	if state.Version == 2 {
		saved = map[string]string{"pr": state.PR, "ref": state.Ref}
	}
	for key, value := range given {
		stored, ok := saved[key]
		if !ok || value != stored {
			return fmt.Errorf("argument %s differs from the saved environment; choose a new --name", key)
		}
	}
	return nil
}
