-- Copy to ~/.config/sup/config.lua. This is trusted code executed on your host.
local sup = require('sup')

local kits = {
  browser = 'ghcr.io/dvdksn/browser-kit:codex',
  task = 'git+https://github.com/docker/sbx-kits-contrib.git#dir=task',
  vale = 'git+https://github.com/docker/sbx-kits-contrib.git#dir=vale',
  signing = 'git+https://github.com/docker/sbx-kits-contrib.git#dir=git-ssh-sign',
  clone = 'git+https://github.com/cdupuis/sbx-kits.git#dir=github-clone',
}
sup.setup({
  kits = kits,
  args = {
    pr = { description = 'Pull request to check out', pattern = '[1-9][0-9]*' },
    ref = { description = 'Branch, tag, or commit' },
  },
  name = function(ctx)
    assert(not (ctx.args.pr and ctx.args.ref), 'pr and ref are mutually exclusive')
    local name = ctx.repo:gsub('/', '-')
    if ctx.args.pr then return name .. '-pr-' .. ctx.args.pr end
    if ctx.args.ref then return name .. '-ref-' .. ctx.args.ref end
    return name
  end,
  defaults = function(ctx)
    assert(not (ctx.args.pr and ctx.args.ref), 'pr and ref are mutually exclusive')
    return {
      schemaVersion = '1',
      agent = 'codex',
      secrets = { github = { command = 'gh auth token' } },
      kits = {
        { source = 'clone', args = {
          repo = ctx.repo, ref = ctx.args.ref or '', pr = ctx.args.pr or '', dir = '/project',
        } },
        'signing',
      },
    }
  end,
  repos = {
    ['docker/docs'] = {
      kits = { 'browser', 'vale' },
      sandboxOptions = { memory = '8g' },
    },
  },
})
