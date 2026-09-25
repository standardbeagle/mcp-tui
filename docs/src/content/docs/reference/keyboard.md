---
title: Keyboard Shortcuts
description: Every keyboard binding in MCP-TUI's terminal interface.
---

## Main screen (connected)

| Key | Action |
|-----|--------|
| `Tab` / `Shift+Tab` | Cycle tabs (Tools → Resources → Prompts → Events) |
| `←` / `→` | Previous / next tab (switches panes in the Events detail view) |
| `↑` / `↓` or `k` / `j` | Move within the list |
| `PgUp` / `PgDn` | Page through the list |
| `Home` / `End` | Jump to first / last item |
| `1`–`9` | Quick-select an item by number |
| `Enter` | Select / activate the item |
| `Ctrl+↑` / `Ctrl+↓` | Scroll the tool description panel |
| `r` | Refresh the current tab; in an open resource viewer, re-read the resource |
| `s` | Watch / stop watching the selected resource (Resources tab; rows show `[watching]`, then `[updated]`) |
| `R` | Open the roots editor |
| `T` | Open the tasks screen |
| `A` | Re-authenticate (clear cached OAuth state) |
| `d` | Disconnect and return to the connection screen |
| `e` | Show schema-error details for the selected tool (Tools tab) |
| `b` / `Alt+←` | Back (closes the Events detail view when open) |
| `Ctrl+E` | Export the session recording (`.json` trace + `.sh` replay script) |
| `Ctrl+L` / `Ctrl+D` / `F12` | Open the debug screen |
| `q` / `Esc` / `Ctrl+C` | Quit |

When a resource or prompt viewer is open, `q` / `Esc` closes the viewer first.

## Tool screen

| Key | Action |
|-----|--------|
| `Tab` / `↓`, `Shift+Tab` / `↑` | Move between fields and buttons |
| `Enter` | Execute (on the Execute button) |
| `Ctrl+T` | Toggle task mode: Execute calls the tool as an MCP task and follows it |
| `Ctrl+O` | Toggle sending arguments that break the input schema: by default such a call is refused; while on (`[schema violations sent]` in the title) it is sent, the violation is shown under the title, and the copied CLI command carries `--skip-arg-validation` |
| `Ctrl+N` | On a nullable field: send null (again to go back to the typed value) |
| `Ctrl+E` | On an object field with declared properties: open it as a sub-form, or close it back to a JSON literal |
| `c` | Show and copy the equivalent CLI command (POSIX shell syntax; each word single-quoted as needed). Server arguments are written one `--arg` each. Raw JSON arguments are written as one `key:=<json>` per top-level key; JSON the CLI cannot take (not an object, or a key other than letters, digits, `_` and `-`) gives a `#` line naming the problem instead of a command |
| `v` | Browse the result's fields |
| `Esc` / `b` | Back |

## Tasks screen

| Key | Action |
|-----|--------|
| `↑` / `↓` or `k` / `j` | Move within the list |
| `Enter` | Open the task's detail; in the detail, fetch its result (waits while it runs) |
| `w` | Fetch the result (detail) |
| `c` | Cancel the task |
| `r` | Refresh from the server |
| `Esc` / `q` | Back to the list, or close |

## Main screen (disconnected)

| Key | Action |
|-----|--------|
| `r` | Retry the connection |
| `b` / `e` | Back to the connection screen to edit details |
| `Ctrl+L` / `Ctrl+D` / `F12` | Open the debug screen |
| `q` / `Esc` / `Ctrl+C` | Quit |

## Connection screen

| Key | Action |
|-----|--------|
| `Tab` | Switch between Saved / Discovery / Manual modes |
| `C` | Toggle combined-command input |
| `1`–`9` | Quick-select a saved / discovered entry |
| `Enter` | Connect |

## Debug screen

| Key | Action |
|-----|--------|
| `Tab` / `→` | Next debug tab |
| `Shift+Tab` / `←` | Previous debug tab |
| `↑` / `↓` or `k` / `j` | Scroll the list (log tabs) |
| `PgUp` / `PgDn` | Page up / down |
| `Home` / `g` | Jump to top |
| `End` / `G` | Jump to bottom |
| `r` | Refresh |
| `Enter` | Open frame detail (MCP Protocol tab) |
| `c` | Copy selected item (copies capabilities JSON on the Capabilities tab; clears logs on the Statistics tab) |
| `y` | Copy selected item / capabilities JSON |
| `Ctrl+E` | Export the session recording (`.json` trace + `.sh` replay script) |
| `Ctrl+C` / `Esc` | Close the debug overlay |
| `b` / `Alt+←` / `Ctrl+D` / `Ctrl+L` / `F12` | Close the debug overlay |

### Notifications tab

| Key | Action |
|-----|--------|
| `Space` / `p` | Pause / resume the notification stream |
| `1`–`8` | Toggle a single notification-type filter (`8` = task status) |
| `0` | Clear all type filters |
| `+` / `=` | Raise the level threshold |
| `-` / `_` | Lower the level threshold |
| `x` | Clear the notification buffer |
</content>
