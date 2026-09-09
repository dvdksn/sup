package sup

import (
	"fmt"
	"io"
	"reflect"
	"sort"
	"text/tabwriter"

	lua "github.com/yuin/gopher-lua"
)

func (c *loadedConfig) kitAliases() (map[string]string, error) {
	aliases := map[string]string{}
	if v := c.table.RawGetString("kits"); v != lua.LNil {
		if err := bind(v, reflect.ValueOf(&aliases).Elem(), "kits", map[*lua.LTable]bool{}); err != nil {
			return nil, err
		}
	}
	return aliases, nil
}

func describeKits(c *loadedConfig, out io.Writer) (int, error) {
	aliases, err := c.kitAliases()
	if err != nil {
		return 1, err
	}
	if len(aliases) == 0 {
		_, err = fmt.Fprintln(out, "No kit aliases configured.")
	} else {
		names := make([]string, 0, len(aliases))
		for name := range aliases {
			names = append(names, name)
		}
		sort.Strings(names)
		w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "NAME\tSOURCE")
		for _, name := range names {
			fmt.Fprintf(w, "%s\t%s\n", display(name), display(aliases[name]))
		}
		err = w.Flush()
	}
	if err != nil {
		return 1, err
	}
	return 0, nil
}
