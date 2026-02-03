# Session Recording Notes (Loop CLI)

## How to record sessions
- Use the local CLI binary: `/home/user/bin/loop`.
- Always include `--network regtest` **after** the main command and subcommands so it does not become part of the session filename.
- Record with `LOOP_SESSION_RECORD=true`, e.g.:
  - `LOOP_SESSION_RECORD=true /home/user/bin/loop quote out --network regtest 500000`
- After recording several related commands, group the resulting JSON files into a subdir under `cmd/loop/testdata/sessions/`.

## Control panel HTTP server (regtest helpers)
Base URL: `http://127.0.0.1:12345`
- Implementation: [httpcmd](https://github.com/starius/httpcmd/)
- Config: [control panel config](https://gist.github.com/starius/6604ffe27f51d55f4cf715b4202637dd)
- `/mine`:
  - Mines a block. Returns JSON with `exit_code`, `stdout`, `stderr`, `duration`, `timed_out`.
- `/deposit`:
  - Sends a deposit to the client static address. Returns JSON with a `txid` on success.
  - May fail with insufficient funds unless blocks have been mined first.
- `/reservation`:
  - Opens a reservation for instant out. Returns JSON with reservation details (id, amount, state, etc.).
- `/loop-log`:
  - Returns loopd logs (text). Use a tail filter when inspecting.
- `/loop-server-log`:
  - Returns loop server logs (text). Use a tail filter when inspecting.

## Useful regtest flow observations
- Deposits may be unconfirmed for a few blocks; mine multiple blocks to move a deposit into `DEPOSITED`.
- After making a deposit, `loop static summary` reflects unconfirmed + confirmed values; `loop static listdeposits` lists confirmed deposits.
- Reservations should be opened (via `/reservation`) before `instantout`; list them with `loop reservations list`.
- `instantout` uses interactive selection; `ALL` then `y` works for a simple scenario:
  - `printf "ALL\ny\n" | LOOP_SESSION_RECORD=true /home/user/bin/loop instantout --network regtest`

## Coverage notes (high-level)
- Sessions were recorded for: terms, getinfo, quote in/out, listauth, fetchl402, getparams, setrule (error + success), suggestswaps (error + success), reservations list, instantout, listinstantouts, static withdraw/listwithdrawals/listswaps, listswaps, swapinfo, loop out (forced), abandon swap (help path), plus existing static-loop-in/basic-swaps.
- Some paths are intentionally skipped for now:
  - `stop` (would shut down loopd; replay expects a real gRPC conn).
  - Asset quote paths (`getAssetAmt`, `unmarshalFixedPoint`) require tapd/asset quotes.
  - Real dialer/macaroon path handling (`extractPathArgs`, `readMacaroon`, `getClientConn`) aren’t exercised by replay.


## Replay stability notes
- Session replay now clones the CLI command tree per run, so flag state (`IsSet`) does not leak between sessions.
- Historical warning: earlier replays could have sticky flags across runs; if you see odd ordering-dependent failures, re-check that the replay uses the cloned command path.

## Session coverage map
| Subdir | Commands / scenarios |
| --- | --- |
| `basic-swaps/` | `loop out` (success), `loop in` (success), `loop monitor` |
| `getinfo/` | `loop getinfo` |
| `instantout/` | `loop reservations list`, `loop instantout` (ALL + confirm), `loop listinstantouts` |
| `l402/` | `loop listauth`, `loop fetchl402` |
| `liquidity/` | `loop getparams`, `loop setparams` (no flags, feepercent, conflict), `loop setrule` (missing threshold, no args, success), `loop suggestswaps` (error + success) |
| `loopin/` | `loop in` (invalid amount) |
| `loopout/` | `loop out` (forced success), `loop out` (invalid amount) |
| `misc/` | `loop terms` |
| `quote/` | `loop quote out` (success), `loop quote in` (help), `loop quote out` (help), `loop quote in` (deposit_outpoint success) |
| `static/` | `loop static withdraw` (no selection error), `loop static listwithdrawals`, `loop static listswaps` |
| `static-loop-in/` | `loop static new`, `loop static` (help), `loop static listunspent` (incl alias), `loop static listdeposits`, `loop static summary`, `loop static in` (multiple args/flags cases) |
| `swaps/` | `loop listswaps` (success + conflicting filters), `loop swapinfo` (success + invalid id), `loop abandonswap` (help + invalid id) |

## Missing / not covered yet
- `loop stop` (would terminate loopd; replay expects a live gRPC server).
- Docs generators: `loop man`, `loop markdown` (handled in a separate docs pipeline).
- Asset/tapd scenarios (explicitly out of scope for now).
- Additional user-error paths not yet recorded: e.g. `loop in` with `--external` + `--conf_target`, `loop in` with `--route_hints` + `--private`, `loop out` with `--addr` + `--account`, `loop static withdraw` success/invalid utxo, `instantout` cancel/invalid selection.
