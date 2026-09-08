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
  defaults = function(ctx)
    return {
      schemaVersion = '1',
      name = ctx.name,
      agent = 'codex',
      secrets = { github = { command = 'gh auth token' } },
      kits = {
        { source = 'clone', args = {
          repo = ctx.repo, ref = ctx.ref, pr = ctx.pr, dir = '/project',
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
