package commands

const sandboxSectionDescription = `Sandboxes are the underlying execution environments for your applications. Most of the time you'll work with apps directly, but these commands are useful for debugging and advanced use cases.`

const sandboxExecDescription = `This command connects to an existing sandbox and runs a command inside it. Unlike ` + "`" + `miren app run` + "`" + ` which creates a new ephemeral sandbox, this connects to a sandbox that's already running (typically one serving production traffic).

## Letting miren pick the sandbox

Pass ` + "`" + `--app` + "`" + ` and you don't need a sandbox ID at all — miren picks one of that app's running sandboxes for you:

` + "```" + `bash
miren sandbox exec --app myapp
miren sandbox exec --app myapp -- ls -la /app
` + "```" + `

The choice is random among the sandboxes that qualify, so with more than one running you may land somewhere different each time. When there was more than one to choose from, miren names the one it picked — on a terminal only, so that piping or redirecting output never mixes miren's chatter into the sandbox's own.

An app with several services gets its ` + "`" + `web` + "`" + ` service, and only that one. If no web instance is up, miren says so rather than substituting a worker, since landing in the wrong kind of process is worse than being told to try again. Use ` + "`" + `--service` + "`" + ` to ask for a different one:

` + "```" + `bash
miren sandbox exec --app myapp --service worker -- ps aux
` + "```" + `

Within the service you asked for, miren prefers sandboxes running your app's **current** version, so a command run during a deploy won't drop you into the version you just replaced. That one is a preference rather than a rule: a failed deploy leaves the current version pointing at something that never came up while the previous instances keep serving traffic, and refusing those would lock you out of a shell exactly when you need one.

:::warning[Put -- before your command]
Anything after ` + "`" + `--` + "`" + ` is passed to the sandbox untouched. Without it, a flag meant for your command is read as a flag for miren — ` + "`" + `miren sandbox exec --app myapp ls -la` + "`" + ` fails on ` + "`" + `-l` + "`" + `, and a ` + "`" + `-h` + "`" + ` anywhere before ` + "`" + `--` + "`" + ` prints this help instead of running anything.
:::

## Finding sandbox IDs

To target a specific sandbox instead, use ` + "`" + `miren sandbox list` + "`" + ` to find its ID:

` + "```" + `bash
$ miren sandbox list
ID                          APP       SERVICE   STATUS    NODE
sandbox/myapp-web-abc123    myapp     web       RUNNING   node-1
sandbox/myapp-web-def456    myapp     web       RUNNING   node-2
` + "```" + `

:::warning[This is a live instance]
When you exec into a production sandbox, you're connecting to a live instance that may be serving traffic. Be careful with commands that could affect the running application.
:::

:::tip[Use app run to stay out of the way]
For debugging or one-off tasks without affecting production, use ` + "`" + `miren app run` + "`" + ` to create an isolated ephemeral sandbox instead.
:::`
