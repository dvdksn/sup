---@meta sup

-- Editor definitions for the embedded sup module; not executable configuration.

---@class (exact) SupEnvironment
---@field schemaVersion? string
---@field name? string
---@field agent string
---@field args? table<string, SupArgument>
---@field kits? (string|SupKit)[]
---@field env? table<string, string>
---@field sandboxOptions? SupSandboxOptions
---@field secrets? table<string, SupSecret>
---@field bindings? table<string, SupBinding>
---@field registries? table<string, SupRegistry>
---@field mcp? SupMCP
---@field ports? SupPort[]
---@field lifecycle? SupLifecycle

---@class (exact) SupEnvironmentOverride
---@field schemaVersion? string
---@field name? string
---@field agent? string
---@field args? table<string, SupArgument>
---@field kits? (string|SupKit)[]
---@field env? table<string, string>
---@field sandboxOptions? SupSandboxOptions
---@field secrets? table<string, SupSecret>
---@field bindings? table<string, SupBinding>
---@field registries? table<string, SupRegistry>
---@field mcp? SupMCP
---@field ports? SupPort[]
---@field lifecycle? SupLifecycle

---@class (exact) SupArgument
---@field default? string
---@field required? boolean
---@field description? string
---@field enum? string[]
---@field pattern? string

---@class (exact) SupKit
---@field source string
---@field args? table<string, string>

---@class (exact) SupSandboxOptions
---@field template? string
---@field memory? string
---@field cpus? integer
---@field pullPolicy? string
---@field profile? string
---@field shareSkills? boolean
---@field display? boolean
---@field gpu? boolean
---@field usb? string[]

---@class (exact) SupSecret
---@field value? string
---@field ref? string
---@field command? string
---@field refresh? string
---@field backend? string
---@field noVerify? boolean

---@class (exact) SupDomains
---@field domains string[]

---@class (exact) SupBinding
---@field apiKey? SupDomains
---@field oauth? SupDomains

---@class (exact) SupRegistry
---@field secret SupSecret
---@field username? SupSecret

---@class (exact) SupMCP
---@field servers SupServer[]

---@class (exact) SupServer
---@field name string
---@field url? string
---@field command? string
---@field args? string[]

---@class (exact) SupPort
---@field sandbox integer
---@field host? integer
---@field protocol? string
---@field hostIP? string

---@class (exact) SupLifecycle
---@field initialize? SupCommand[]
---@field postCreate? SupCommand[]
---@field preRemove? SupCommand[]

---@class (exact) SupCommand
---@field name? string
---@field command string
---@field workdir? string
---@field timeout? string

---@class (exact) SupContext
---@field repo string GitHub owner/repository.
---@field name string Resolved sandbox name.
---@field ref string Empty when not selected.
---@field pr string PR number, or empty when not selected.

---@class (exact) SupConfig
---@field kits? table<string, string> Friendly aliases for kit sources.
---@field defaults? SupEnvironment|fun(ctx: SupContext): SupEnvironment
---@field repos? table<string, SupEnvironmentOverride> Exact repository matches.
---@field configure? fun(ctx: SupContext): SupEnvironment Legacy; cannot be combined with defaults or repos.

local sup = {}

---Register one configuration. The returned environment is validated at runtime.
---@param config SupConfig
function sup.setup(config) end

return sup
