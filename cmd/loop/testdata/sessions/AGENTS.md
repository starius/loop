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
- CLI flag instances are global; in session replay they can retain `IsSet` state across runs.
- Avoid recording sessions that set flags which would change later runs (e.g. `--route_hints`, `--private`, `--addr`, `--account`, `--utxo`) unless they are ordered after all other sessions that use those flags.
- Prefer error scenarios that do not set flags (invalid amount, conflicting IDs, missing args/help paths) to keep replay deterministic.
- If you must keep multiple sessions for the same command with different flags, order filenames so the least "sticky" runs first and the most flag-heavy runs last.
