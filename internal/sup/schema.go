package sup

// These types describe the native sbxenv format; sbx performs final semantic validation.
type Environment struct {
	SchemaVersion  string              `json:"schemaVersion"`
	Name           string              `json:"name"`
	Agent          string              `json:"agent"`
	Args           map[string]Argument `json:"args,omitempty"`
	Kits           []Kit               `json:"kits"`
	Env            map[string]string   `json:"env,omitempty"`
	SandboxOptions *SandboxOptions     `json:"sandboxOptions,omitempty"`
	Secrets        map[string]Secret   `json:"secrets,omitempty"`
	Bindings       map[string]Binding  `json:"bindings,omitempty"`
	Registries     map[string]Registry `json:"registries,omitempty"`
	MCP            *MCP                `json:"mcp,omitempty"`
	Ports          []Port              `json:"ports,omitempty"`
	Lifecycle      *Lifecycle          `json:"lifecycle,omitempty"`
}
type Argument struct {
	Default     *string  `json:"default,omitempty"`
	Required    bool     `json:"required,omitempty"`
	Description string   `json:"description,omitempty"`
	Enum        []string `json:"enum,omitempty"`
	Pattern     string   `json:"pattern,omitempty"`
}
type Kit struct {
	Source string            `json:"source"`
	Args   map[string]string `json:"args,omitempty"`
}
type SandboxOptions struct {
	Template    string   `json:"template,omitempty"`
	Memory      string   `json:"memory,omitempty"`
	CPUs        int      `json:"cpus,omitempty"`
	PullPolicy  string   `json:"pullPolicy,omitempty"`
	Profile     string   `json:"profile,omitempty"`
	ShareSkills *bool    `json:"shareSkills,omitempty"`
	Display     bool     `json:"display,omitempty"`
	GPU         bool     `json:"gpu,omitempty"`
	USB         []string `json:"usb,omitempty"`
}
type Secret struct {
	Value    *string `json:"value,omitempty"`
	Ref      string  `json:"ref,omitempty"`
	Command  string  `json:"command,omitempty"`
	Refresh  string  `json:"refresh,omitempty"`
	Backend  string  `json:"backend,omitempty"`
	NoVerify bool    `json:"noVerify,omitempty"`
}
type Domains struct {
	Domains []string `json:"domains"`
}
type Binding struct {
	APIKey *Domains `json:"apiKey,omitempty"`
	OAuth  *Domains `json:"oauth,omitempty"`
}
type Registry struct {
	Secret   Secret  `json:"secret"`
	Username *Secret `json:"username,omitempty"`
}
type MCP struct {
	Servers []Server `json:"servers"`
}
type Server struct {
	Name    string   `json:"name"`
	URL     string   `json:"url,omitempty"`
	Command string   `json:"command,omitempty"`
	Args    []string `json:"args,omitempty"`
}
type Port struct {
	Sandbox  int    `json:"sandbox"`
	Host     int    `json:"host,omitempty"`
	Protocol string `json:"protocol,omitempty"`
	HostIP   string `json:"hostIP,omitempty"`
}
type Lifecycle struct {
	Initialize []Command `json:"initialize,omitempty"`
	PostCreate []Command `json:"postCreate,omitempty"`
	PreRemove  []Command `json:"preRemove,omitempty"`
}
type Command struct {
	Name    string `json:"name,omitempty"`
	Command string `json:"command"`
	Workdir string `json:"workdir,omitempty"`
	Timeout string `json:"timeout,omitempty"`
}
