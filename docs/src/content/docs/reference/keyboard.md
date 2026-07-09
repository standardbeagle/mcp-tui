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
| `r` | Refresh the current tab |
| `R` | Open the roots editor |
| `A` | Re-authenticate (clear cached OAuth state) |
| `d` | Disconnect and return to the connection screen |
| `e` | Show schema-error details for the selected tool (Tools tab) |
| `b` / `Alt+←` | Back (closes the Events detail view when open) |
| `Ctrl+E` | Export the session recording (`.json` trace + `.sh` replay script) |
| `Ctrl+L` / `Ctrl+D` / `F12` | Open the debug screen |
| `q` / `Esc` / `Ctrl+C` | Quit |

When a resource or prompt viewer is open, `q` / `Esc` closes the viewer first.

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
| `1`–`7` | Toggle a single notification-type filter |
| `0` | Clear all type filters |
| `+` / `=` | Raise the level threshold |
| `-` / `_` | Lower the level threshold |
| `x` | Clear the notification buffer |
</content>
