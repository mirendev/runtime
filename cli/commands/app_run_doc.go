package commands

const appRunDescription = `This command runs a command in a fresh sandbox built from your app's active version — the same image, environment variables, and working directory as your deployed app — and connects your terminal to it.

With no arguments it opens an interactive shell. With arguments it runs that command. With ` + "`" + `--task` + "`" + ` it runs a task declared in ` + "`" + `app.toml` + "`" + `.

This is useful for:
- Debugging application issues in an isolated environment
- Running one-off commands with your app's configuration
- Running a declared task by hand, such as re-running a migration without redeploying
- Exploring the container filesystem

### How It Works

1. Miren records a **run**: a durable entity holding what was executed, by whom, and how it ended
2. The run controller builds a sandbox from your app's active version and starts the command
3. Your terminal attaches to that command
4. On exit, the command's exit code is recorded and propagated to your shell, and the sandbox is torn down

### Runs outlive your terminal

Disconnecting is not cancelling. If your connection drops, the command keeps going — reattach with ` + "`" + `miren app attach` + "`" + `, read its output with ` + "`" + `miren logs run` + "`" + `, or leave it and let its timeout reap it.

` + "`" + `--detach` + "`" + ` therefore means exactly one thing: don't attach my terminal right now. Use ` + "`" + `miren app runs cancel` + "`" + ` to actually stop a run.

:::tip
The sandbox is per-invocation. Any changes you make (files created, packages installed) are discarded when it ends.
:::

:::note
Runs are recorded, so ` + "`" + `miren app runs` + "`" + ` shows what has been executed against an app and how it ended. To reach into an already-running production sandbox instead, use ` + "`" + `miren sandbox exec` + "`" + `.
:::`

const appAttachDescription = `Joins your terminal to a task run that is already going.

Attaching and detaching are invisible to the run itself: it is started by the platform and outlives any client. So you can attach to a run started with ` + "`" + `--detach` + "`" + `, reattach after a dropped connection, or never attach at all.

Several clients can attach to one run at once; they share the same terminal.

:::note
Only runs whose command is still executing can be attached. For one that has finished, read its output back with ` + "`" + `miren logs run` + "`" + `.
:::`
